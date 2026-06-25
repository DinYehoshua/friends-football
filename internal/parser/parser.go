package parser

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"

	"friends-football/internal/database"
)

const (
	apiKeyEnvVar    = "GEMINI_API_KEY"
	expectedCount   = 12
	chatFileName    = "_chat.txt"
	chatHistoryDays = 8 // Number of days of chat history to keep
)

// WhatsApp date pattern: [DD/MM/YYYY, HH:MM:SS] (with optional Unicode control chars at start)
// The \p{Cf} matches Unicode "format" characters like U+200E (LTR mark), U+200F (RTL mark)
var whatsappDateRegex = regexp.MustCompile(`^[\p{Cf}\s]*\[(\d{2}/\d{2}/\d{4}), \d{1,2}:\d{2}:\d{2}\]`)

// Models to try in order (fallback on rate limit errors)
var modelFallbackOrder = []string{
	"gemini-3.1-flash-lite", // Primary - auto-updates to newest
	"gemini-flash-latest",   // First fallback
	"gemini-2.5-flash",      // Second fallback
	"gemini-2.5-flash-lite", // Final fallback
}

var (
	ErrNoAPIKey           = errors.New("GEMINI_API_KEY environment variable not set")
	ErrInvalidResponse    = errors.New("invalid response from Gemini")
	ErrPlayerCountInvalid = errors.New("expected exactly 12 players from chat")
	ErrChatFileNotFound   = errors.New("_chat.txt not found in zip archive")
	ErrInvalidZip         = errors.New("invalid zip file")
	ErrAIOverloaded       = errors.New("ai_overloaded") // All models rate limited
)

// Parser handles WhatsApp chat parsing using Gemini LLM.
type Parser struct {
	client *genai.Client
}

// UnresolvedPlayer represents a player name that couldn't be matched to the database.
type UnresolvedPlayer struct {
	ExtractedName string
	Index         int
}

// ParseResult contains the parsing outcome with resolved and unresolved players.
type ParseResult struct {
	Players           []database.Player
	UnresolvedPlayers []UnresolvedPlayer
	ExtractedNames    []string
}

// New creates a new Parser with Gemini client.
func New(ctx context.Context) (*Parser, error) {
	apiKey := os.Getenv(apiKeyEnvVar)
	if apiKey == "" {
		return nil, ErrNoAPIKey
	}

	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	return &Parser{
		client: client,
	}, nil
}

// NewWithClient creates a Parser with a pre-configured client (for testing).
func NewWithClient(client *genai.Client) *Parser {
	return &Parser{
		client: client,
	}
}

// createModel creates and configures a model by name.
func (p *Parser) createModel(modelName string) *genai.GenerativeModel {
	model := p.client.GenerativeModel(modelName)
	model.ResponseMIMEType = "application/json"
	model.ResponseSchema = &genai.Schema{
		Type: genai.TypeArray,
		Items: &genai.Schema{
			Type: genai.TypeString,
		},
		Description: "Array of exactly 12 player names/nicknames attending the match",
	}
	temp := float32(0.1) // Low temperature for consistent parsing
	model.Temperature = &temp
	return model
}

// Close releases the Gemini client resources.
func (p *Parser) Close() error {
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}

// ExtractChatFromZip reads a WhatsApp export zip file and extracts the _chat.txt content.
func ExtractChatFromZip(zipPath string) (string, error) {
	zipData, err := os.ReadFile(zipPath)
	if err != nil {
		return "", fmt.Errorf("failed to read zip file: %w", err)
	}
	return ExtractChatFromZipBytes(zipData)
}

// ExtractChatFromZipBytes extracts _chat.txt content from zip file bytes.
// This is useful when receiving the zip as an upload ([]byte).
func ExtractChatFromZipBytes(zipData []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidZip, err)
	}

	for _, file := range reader.File {
		if file.Name == chatFileName || strings.HasSuffix(file.Name, chatFileName) {
			rc, err := file.Open()
			if err != nil {
				return "", fmt.Errorf("failed to open %s: %w", chatFileName, err)
			}
			defer rc.Close()

			content, err := io.ReadAll(rc)
			if err != nil {
				return "", fmt.Errorf("failed to read %s: %w", chatFileName, err)
			}

			return string(content), nil
		}
	}

	return "", ErrChatFileNotFound
}

// TrimChatHistory trims a WhatsApp chat export to only include the last N days
// of messages (default 8 days) from the CURRENT system date. This prevents
// token limit issues when parsing long chat histories.
//
// The function iterates from the END of the chat (most efficient for large histories)
// to find the cutoff point, properly handling multiline messages.
func TrimChatHistory(chatContent string) string {
	lines := strings.Split(chatContent, "\n")
	if len(lines) == 0 {
		return chatContent
	}

	// Calculate cutoff date (8 days before TODAY), truncated to midnight for proper comparison
	now := time.Now()
	cutoffDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -chatHistoryDays)
	log.Printf("[Parser] Trimming chat: today=%s, cutoff=%s", now.Format("02/01/2006"), cutoffDate.Format("02/01/2006"))

	// Iterate backwards from the end to find where the cutoff starts
	// This is O(recent_messages) instead of O(total_messages)
	// Track the index of the first valid (within range) message we've seen
	firstValidMessageIndex := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if date, ok := parseWhatsAppDate(lines[i]); ok {
			if date.Before(cutoffDate) {
				// Found message BEFORE cutoff - stop here
				break
			}
			// This message is within range - remember it
			firstValidMessageIndex = i
		}
	}

	// No valid messages found: either no dates at all, or all messages too old
	if firstValidMessageIndex == -1 {
		log.Println("[Parser] No messages within cutoff period")
		return ""
	}

	// Join from the first valid message to end
	trimmedLines := lines[firstValidMessageIndex:]
	trimmedContent := strings.Join(trimmedLines, "\n")

	log.Printf("[Parser] Trimmed chat from %d to %d lines", len(lines), len(trimmedLines))
	return trimmedContent
}

// parseWhatsAppDate extracts the date from a WhatsApp message line.
// Returns the parsed date and true if successful, zero time and false otherwise.
func parseWhatsAppDate(line string) (time.Time, bool) {
	matches := whatsappDateRegex.FindStringSubmatch(line)
	if len(matches) < 2 {
		return time.Time{}, false
	}

	// Parse DD/MM/YYYY format
	date, err := time.Parse("02/01/2006", matches[1])
	if err != nil {
		return time.Time{}, false
	}

	return date, true
}

// systemPrompt returns the specialized prompt for WhatsApp chat parsing.
func systemPrompt() string {
	return `You are a WhatsApp football group chat analyzer. Your task is to extract exactly 12 players who are confirmed to attend the upcoming Saturday match.

LANGUAGE: The chat is primarily in HEBREW. Keep all names in their original language - do NOT translate Hebrew names to English.

MOST IMPORTANT - THE RUNNING COUNT SYSTEM:
Players track attendance by posting a running count number. This number represents the TOTAL registered players after their message:
- "עומר: 1" = Omer registers, total is now 1
- "דן: 2" = Dan registers, total is now 2
- "ניב: 3" = Niv registers, total is now 3
- "דין: דוד מגיע 7" = Din brings his friend David, total is now 7 (Din was already counted, David is new)
- "משה 11" = Moshe registers, total is now 11
- "סורי נפצעתי 11" = Someone cancels, count drops back to 11 (was 12)
- "לא יכול 10" = Another cancellation, count drops to 10

The number ALWAYS reflects the new total after that action. When count goes UP = registration. When count goes DOWN = cancellation.

HANDLING FRIENDS/GUESTS - CRITICAL:
When someone brings a friend without specifying a name, create a name in the format "חבר X" (Friend X):
- "דין: מביא חבר 7" or "דין: חבר 7" → extract "חבר דין"
- "עומר: +1 8" → extract "חבר עומר"
- "ניב: friend 9" → extract "חבר ניב"
- If a friend's name IS given: "דין: יוסי מגיע 7" → extract "יוסי" (the actual name)

HANDLING EDGE CASES & HUMAN ERRORS:
People often forget to write numbers or make mistakes. You must use context to understand intent:
- "אני בפנים" or "נרשם" without a number = registration (infer from message order)
- "סורי לא יכול" without a number = cancellation (identify who sent it)
- Wrong numbers (e.g., someone writes "5" when it should be "6") = use message flow to determine actual count
- Typos and informal language are common - interpret the intent
- If the count seems off, trust the flow of registrations/cancellations over the stated number

ADDITIONAL RULES:
1. Focus ONLY on the most recent registration cycle. Registration resets every Saturday at 20:00.
2. IGNORE completely:
   - System messages ("X added Y to the group", "X changed the group description")
   - General banter, jokes, and off-topic messages
   - Old registration cycles (before the most recent Saturday 20:00)
3. If someone registers and later cancels, they are OUT
4. If someone cancels and later re-registers, they are IN
5. Return EXACTLY 12 player names/nicknames as they appear in the chat
6. Keep names in Hebrew if they appear in Hebrew - do NOT transliterate or translate
7. Use the name/nickname as written in the chat (may be first name, nickname, or phone number)

OUTPUT: Return a JSON array of exactly 12 strings - the final confirmed attendees in their original language.`
}

// IsRateLimitError checks if the error is a rate limit (429) or quota error.
func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "quota") ||
		strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "resource exhausted")
}

// IsFatalError checks if the error should stop all retries immediately.
func IsFatalError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	// Context cancelled/deadline exceeded - no point retrying
	if strings.Contains(errStr, "context canceled") ||
		strings.Contains(errStr, "context deadline exceeded") {
		return true
	}
	// Invalid API key - all models will fail
	if strings.Contains(errStr, "api key") ||
		strings.Contains(errStr, "invalid key") ||
		strings.Contains(errStr, "unauthorized") ||
		strings.Contains(errStr, "403") {
		return true
	}
	return false
}

// ParseChat extracts 12 attending players from a WhatsApp chat text content.
// It tries multiple models in order, falling back only on rate limit errors.
func (p *Parser) ParseChat(ctx context.Context, chatContent string) ([]string, error) {
	// Trim chat history to last 8 days to avoid token limit issues
	trimmedContent := TrimChatHistory(chatContent)
	prompt := fmt.Sprintf("%s\n\nWhatsApp Chat Content:\n%s", systemPrompt(), trimmedContent)

	var lastErr error

	for _, modelName := range modelFallbackOrder {
		log.Printf("[Parser] Trying model: %s", modelName)
		model := p.createModel(modelName)

		resp, err := model.GenerateContent(ctx, genai.Text(prompt))
		if err != nil {
			lastErr = err

			// Fatal errors - stop immediately, no point trying other models
			if IsFatalError(err) {
				log.Printf("[Parser] Fatal error on %s: %v", modelName, err)
				return nil, ErrAIOverloaded
			}

			// Rate limit - try next model
			if IsRateLimitError(err) {
				log.Printf("[Parser] Rate limit hit on %s, trying next model...", modelName)
				continue
			}

			// Other errors - also try next model (might be model-specific issue)
			log.Printf("[Parser] Error on %s: %v, trying next model...", modelName, err)
			continue
		}

		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			return nil, ErrInvalidResponse
		}

		// Extract text from response
		textPart, ok := resp.Candidates[0].Content.Parts[0].(genai.Text)
		if !ok {
			return nil, ErrInvalidResponse
		}

		// Parse JSON array
		var names []string
		if err := json.Unmarshal([]byte(textPart), &names); err != nil {
			return nil, fmt.Errorf("failed to parse JSON response: %w", err)
		}

		if len(names) != expectedCount {
			return nil, fmt.Errorf("%w: got %d", ErrPlayerCountInvalid, len(names))
		}

		log.Printf("[Parser] Successfully extracted %d players using model %s", len(names), modelName)
		return names, nil
	}

	// All models failed
	log.Printf("[Parser] All models failed, last error: %v", lastErr)
	return nil, ErrAIOverloaded
}

// ParseChatFromZip extracts chat from zip file and parses it.
func (p *Parser) ParseChatFromZip(ctx context.Context, zipPath string) ([]string, error) {
	chatContent, err := ExtractChatFromZip(zipPath)
	if err != nil {
		return nil, err
	}
	return p.ParseChat(ctx, chatContent)
}

// ParseChatFromZipBytes extracts chat from zip bytes and parses it.
func (p *Parser) ParseChatFromZipBytes(ctx context.Context, zipData []byte) ([]string, error) {
	chatContent, err := ExtractChatFromZipBytes(zipData)
	if err != nil {
		return nil, err
	}
	return p.ParseChat(ctx, chatContent)
}

// ResolvePlayersFromDB matches extracted names against database players.
func ResolvePlayersFromDB(extractedNames []string) (*ParseResult, error) {
	allPlayers, err := getAllPlayers()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch players: %w", err)
	}

	result := &ParseResult{
		Players:        make([]database.Player, 0, len(extractedNames)),
		ExtractedNames: extractedNames,
	}

	for i, name := range extractedNames {
		player, found := matchPlayer(name, allPlayers)
		if found {
			result.Players = append(result.Players, player)
		} else {
			result.UnresolvedPlayers = append(result.UnresolvedPlayers, UnresolvedPlayer{
				ExtractedName: name,
				Index:         i,
			})
			// Add placeholder for unresolved player
			result.Players = append(result.Players, database.Player{
				ID:                -1,
				Name:              name,
				Phone:             "UNRESOLVED",
				BaseSkillRating:   5.0,
				BaseFitnessRating: 3.0, // Default to Average (3)
			})
		}
	}

	if len(result.UnresolvedPlayers) > 0 {
		log.Printf("Warning: %d players could not be resolved from database", len(result.UnresolvedPlayers))
	}

	return result, nil
}

// getAllPlayers fetches all players from the database.
func getAllPlayers() ([]database.Player, error) {
	rows, err := database.DB.Query(`
		SELECT id, name, phone, email, nickname_aliases, base_skill_rating, base_fitness_rating
		FROM players
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var players []database.Player
	for rows.Next() {
		var p database.Player
		if err := rows.Scan(&p.ID, &p.Name, &p.Phone, &p.Email, &p.NicknameAliases, &p.BaseSkillRating, &p.BaseFitnessRating); err != nil {
			return nil, err
		}
		players = append(players, p)
	}

	return players, rows.Err()
}

// matchPlayer attempts to match an extracted name against database players.
// Supports Hebrew and other Unicode characters. Matching is done by:
// 1. Exact match on name (case-insensitive)
// 2. Exact match on phone number
// 3. Exact match on any nickname alias
// 4. Partial match - extracted name contains or is contained in player name/alias
func matchPlayer(extractedName string, players []database.Player) (database.Player, bool) {
	normalizedName := normalizeString(extractedName)
	if normalizedName == "" {
		return database.Player{}, false
	}

	// First pass: exact matches (highest priority)
	for _, player := range players {
		if normalizeString(player.Name) == normalizedName {
			return player, true
		}
		if normalizeString(player.Phone) == normalizedName {
			return player, true
		}
		if matchesAlias(player, normalizedName, true) {
			return player, true
		}
	}

	// Second pass: partial matches (for cases where contact names differ)
	// e.g., "דין" should match "Din Y" or "דין יוסף"
	for _, player := range players {
		playerName := normalizeString(player.Name)
		// Check if extracted name contains player name or vice versa
		if len(normalizedName) >= 2 && len(playerName) >= 2 {
			if strings.Contains(normalizedName, playerName) || strings.Contains(playerName, normalizedName) {
				return player, true
			}
		}
		if matchesAlias(player, normalizedName, false) {
			return player, true
		}
	}

	return database.Player{}, false
}

// matchesAlias checks if the normalized name matches any of the player's aliases.
// If exactOnly is true, only exact matches are considered.
// If exactOnly is false, partial matches (contains) are also considered.
func matchesAlias(player database.Player, normalizedName string, exactOnly bool) bool {
	if !player.NicknameAliases.Valid {
		return false
	}

	var aliases []string
	if err := json.Unmarshal([]byte(player.NicknameAliases.String), &aliases); err != nil {
		return false
	}

	for _, alias := range aliases {
		normalizedAlias := normalizeString(alias)
		if normalizedAlias == normalizedName {
			return true
		}
		if !exactOnly && len(normalizedName) >= 2 && len(normalizedAlias) >= 2 {
			if strings.Contains(normalizedName, normalizedAlias) || strings.Contains(normalizedAlias, normalizedName) {
				return true
			}
		}
	}

	return false
}

// normalizeString prepares a string for comparison by lowercasing and trimming.
func normalizeString(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ParseAndResolve parses chat content and resolves players in one call.
func (p *Parser) ParseAndResolve(ctx context.Context, chatContent string) (*ParseResult, error) {
	names, err := p.ParseChat(ctx, chatContent)
	if err != nil {
		return nil, err
	}
	return ResolvePlayersFromDB(names)
}
