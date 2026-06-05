package parser_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"friends-football/internal/database"
	"friends-football/internal/parser"
)

func TestParser(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Parser Suite")
}

// Sample chaotic WhatsApp chat with noise, increments, and cancellations
const sampleChaoticChat = `[20/05/2026, 20:05:32] Omer: Hey guys, registration for Saturday is open!
[20/05/2026, 20:06:15] Omer: 1
[20/05/2026, 20:08:45] Dan: 2
[20/05/2026, 20:10:22] System: Niv was added by Omer
[20/05/2026, 20:12:33] Niv: 3
[20/05/2026, 20:15:00] Yossi: 4
[20/05/2026, 20:18:45] Noam: 5
[20/05/2026, 20:20:11] Eyal: Anyone watching the game tonight?
[20/05/2026, 20:22:33] Dan: Yeah, should be a good one!
[20/05/2026, 20:25:00] Amit: my friend David 7
[20/05/2026, 20:30:15] Roi: 8
[20/05/2026, 20:35:22] Gal: 9
[20/05/2026, 20:40:00] Tomer: 10
[21/05/2026, 08:15:33] Noam: Sorry guys, I can't make it Saturday 9
[21/05/2026, 09:00:00] Oren: 10
[21/05/2026, 10:30:45] Ben: 11
[21/05/2026, 12:00:00] System: Omer changed the group description
[21/05/2026, 14:22:11] Lior: 12 - we're full!
[21/05/2026, 15:00:00] Shai: Too late? Put me on waitlist
[21/05/2026, 16:30:00] Omer: @Shai yes sorry, we're at 12
[22/05/2026, 09:00:00] Amit: Hey guys, David can't make it anymore 11
[22/05/2026, 09:05:00] Shai: Great, I'll take that spot! 12
[22/05/2026, 18:00:00] Eyal: Have fun tomorrow everyone!`

var expectedPlayers = []string{
	"Omer", "Dan", "Niv", "Yossi", "Amit", "Roi",
	"Gal", "Tomer", "Oren", "Ben", "Lior", "Shai",
}

var _ = Describe("Parser", func() {
	Context("IsRateLimitError", func() {
		It("detects 429 status code errors", func() {
			err := errors.New("googleapi: Error 429: You exceeded your current quota")
			Expect(parser.IsRateLimitError(err)).To(BeTrue())
		})

		It("detects quota exceeded errors", func() {
			err := errors.New("Quota exceeded for metric: generativelanguage.googleapis.com")
			Expect(parser.IsRateLimitError(err)).To(BeTrue())
		})

		It("detects rate limit text", func() {
			err := errors.New("rate limit exceeded, please retry")
			Expect(parser.IsRateLimitError(err)).To(BeTrue())
		})

		It("detects resource exhausted errors", func() {
			err := errors.New("Resource exhausted: too many requests")
			Expect(parser.IsRateLimitError(err)).To(BeTrue())
		})

		It("returns false for nil error", func() {
			Expect(parser.IsRateLimitError(nil)).To(BeFalse())
		})

		It("returns false for unrelated errors", func() {
			err := errors.New("connection refused")
			Expect(parser.IsRateLimitError(err)).To(BeFalse())
		})

		It("is case insensitive", func() {
			err := errors.New("QUOTA EXCEEDED")
			Expect(parser.IsRateLimitError(err)).To(BeTrue())
		})
	})

	Context("IsFatalError", func() {
		It("detects context canceled", func() {
			err := errors.New("context canceled")
			Expect(parser.IsFatalError(err)).To(BeTrue())
		})

		It("detects context deadline exceeded", func() {
			err := errors.New("context deadline exceeded")
			Expect(parser.IsFatalError(err)).To(BeTrue())
		})

		It("detects API key errors", func() {
			err := errors.New("invalid API key provided")
			Expect(parser.IsFatalError(err)).To(BeTrue())
		})

		It("detects unauthorized errors", func() {
			err := errors.New("unauthorized: invalid credentials")
			Expect(parser.IsFatalError(err)).To(BeTrue())
		})

		It("detects 403 forbidden errors", func() {
			err := errors.New("googleapi: Error 403: Permission denied")
			Expect(parser.IsFatalError(err)).To(BeTrue())
		})

		It("returns false for nil error", func() {
			Expect(parser.IsFatalError(nil)).To(BeFalse())
		})

		It("returns false for rate limit errors", func() {
			err := errors.New("429 rate limit exceeded")
			Expect(parser.IsFatalError(err)).To(BeFalse())
		})

		It("returns false for generic errors", func() {
			err := errors.New("some network error")
			Expect(parser.IsFatalError(err)).To(BeFalse())
		})

		It("is case insensitive", func() {
			err := errors.New("CONTEXT CANCELED")
			Expect(parser.IsFatalError(err)).To(BeTrue())
		})
	})

	Context("ErrAIOverloaded", func() {
		It("has the expected error message", func() {
			Expect(parser.ErrAIOverloaded).To(MatchError("ai_overloaded"))
		})
	})

	Context("ResolvePlayersFromDB", func() {
		BeforeEach(func() {
			err := database.Init(":memory:")
			Expect(err).NotTo(HaveOccurred())
			seedTestPlayers()
		})

		AfterEach(func() {
			database.Close()
		})

		When("all names match database players", func() {
			It("resolves all players correctly", func() {
				extractedNames := []string{
					"Omer", "Dan", "Niv", "Yossi", "Amit", "Roi",
					"Gal", "Tomer", "Oren", "Ben", "Lior", "Shai",
				}

				result, err := parser.ResolvePlayersFromDB(extractedNames)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Players).To(HaveLen(12))
				Expect(result.UnresolvedPlayers).To(BeEmpty())

				for _, p := range result.Players {
					Expect(p.ID).To(BeNumerically(">", 0))
				}
			})
		})

		When("names match via nickname aliases", func() {
			It("resolves players using their aliases", func() {
				extractedNames := []string{
					"Omeri", "Danny", "Niv", "Yossi", "Amit", "Roi",
					"Gal", "Tomer", "Oren", "Ben", "Lior", "Shai",
				}

				result, err := parser.ResolvePlayersFromDB(extractedNames)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.UnresolvedPlayers).To(BeEmpty())
				Expect(result.Players[0].Name).To(Equal("Omer"))
				Expect(result.Players[1].Name).To(Equal("Dan"))
			})
		})

		When("some names cannot be resolved", func() {
			It("creates placeholder players for unresolved names", func() {
				extractedNames := []string{
					"Omer", "Dan", "Niv", "Yossi", "Amit", "Roi",
					"Gal", "Tomer", "Oren", "Ben", "UnknownPlayer1", "UnknownPlayer2",
				}

				result, err := parser.ResolvePlayersFromDB(extractedNames)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Players).To(HaveLen(12))
				Expect(result.UnresolvedPlayers).To(HaveLen(2))

				Expect(result.UnresolvedPlayers[0].ExtractedName).To(Equal("UnknownPlayer1"))
				Expect(result.UnresolvedPlayers[0].Index).To(Equal(10))
				Expect(result.Players[10].ID).To(Equal(-1))
				Expect(result.Players[11].ID).To(Equal(-1))
			})
		})

		When("names have different casing", func() {
			It("resolves names case-insensitively", func() {
				extractedNames := []string{
					"OMER", "dan", "NiV", "YOSSI", "amit", "ROI",
					"gal", "TOMER", "oren", "BEN", "lior", "SHAI",
				}

				result, err := parser.ResolvePlayersFromDB(extractedNames)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.UnresolvedPlayers).To(BeEmpty())
			})
		})

		When("names have extra whitespace", func() {
			It("resolves names after trimming", func() {
				extractedNames := []string{
					"  Omer  ", "Dan ", " Niv", "Yossi", "Amit", "Roi",
					"Gal", "Tomer", "Oren", "Ben", "Lior", "Shai",
				}

				result, err := parser.ResolvePlayersFromDB(extractedNames)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.UnresolvedPlayers).To(BeEmpty())
			})
		})

		When("matching by phone number", func() {
			It("resolves players by their phone number", func() {
				extractedNames := []string{
					"+972501111111", "Dan", "Niv", "Yossi", "Amit", "Roi",
					"Gal", "Tomer", "Oren", "Ben", "Lior", "Shai",
				}

				result, err := parser.ResolvePlayersFromDB(extractedNames)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.UnresolvedPlayers).To(BeEmpty())
				Expect(result.Players[0].Name).To(Equal("Omer"))
			})
		})

		When("matching Hebrew names", func() {
			BeforeEach(func() {
				// Add Hebrew players
				database.DB.Exec(`
					INSERT INTO players (name, phone, nickname_aliases, base_skill_rating, base_fitness_rating)
					VALUES ('דין יוסף', '+972509876543', '["דין", "Din", "דיני"]', 7.0, 2.0)
				`)
				database.DB.Exec(`
					INSERT INTO players (name, phone, nickname_aliases, base_skill_rating, base_fitness_rating)
					VALUES ('גל כהן', '+972508765432', '["גל", "גלי", "Gal K"]', 6.0, 2.0)
				`)
				database.DB.Exec(`
					INSERT INTO players (name, phone, nickname_aliases, base_skill_rating, base_fitness_rating)
					VALUES ('אורן ברוידא', '+972507654321', '["אורן", "Oren B"]', 8.0, 3.0)
				`)
			})

			It("resolves exact Hebrew names", func() {
				extractedNames := []string{
					"דין יוסף", "גל כהן", "אורן ברוידא", "Yossi", "Amit", "Roi",
					"Gal", "Tomer", "Oren", "Ben", "Lior", "Shai",
				}

				result, err := parser.ResolvePlayersFromDB(extractedNames)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Players[0].Name).To(Equal("דין יוסף"))
				Expect(result.Players[1].Name).To(Equal("גל כהן"))
				Expect(result.Players[2].Name).To(Equal("אורן ברוידא"))
			})

			It("resolves Hebrew aliases", func() {
				extractedNames := []string{
					"דין", "גלי", "אורן", "Yossi", "Amit", "Roi",
					"Gal", "Tomer", "Oren", "Ben", "Lior", "Shai",
				}

				result, err := parser.ResolvePlayersFromDB(extractedNames)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Players[0].Name).To(Equal("דין יוסף"))
				Expect(result.Players[1].Name).To(Equal("גל כהן"))
				Expect(result.Players[2].Name).To(Equal("אורן ברוידא"))
			})

			It("resolves partial Hebrew names via substring match", func() {
				extractedNames := []string{
					"דין יוסף הגדול", "Dan", "Niv", "Yossi", "Amit", "Roi",
					"Gal", "Tomer", "Oren", "Ben", "Lior", "Shai",
				}

				result, err := parser.ResolvePlayersFromDB(extractedNames)
				Expect(err).NotTo(HaveOccurred())
				// "דין יוסף הגדול" contains "דין יוסף"
				Expect(result.Players[0].Name).To(Equal("דין יוסף"))
			})

			It("resolves mixed Hebrew and English aliases", func() {
				extractedNames := []string{
					"Din", "Gal K", "Oren B", "Yossi", "Amit", "Roi",
					"Gal", "Tomer", "Oren", "Ben", "Lior", "Shai",
				}

				result, err := parser.ResolvePlayersFromDB(extractedNames)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Players[0].Name).To(Equal("דין יוסף"))
				Expect(result.Players[1].Name).To(Equal("גל כהן"))
				Expect(result.Players[2].Name).To(Equal("אורן ברוידא"))
			})
		})
	})

	Context("ExtractChatFromZip", func() {
		When("given a valid zip with _chat.txt", func() {
			It("extracts the chat content", func() {
				zipData := createTestZip("_chat.txt", sampleChaoticChat)

				content, err := parser.ExtractChatFromZipBytes(zipData)
				Expect(err).NotTo(HaveOccurred())
				Expect(content).To(Equal(sampleChaoticChat))
			})
		})

		When("given a zip without _chat.txt", func() {
			It("returns ErrChatFileNotFound", func() {
				zipData := createTestZip("other.txt", "some content")

				_, err := parser.ExtractChatFromZipBytes(zipData)
				Expect(err).To(MatchError(parser.ErrChatFileNotFound))
			})
		})

		When("given invalid zip data", func() {
			It("returns ErrInvalidZip", func() {
				_, err := parser.ExtractChatFromZipBytes([]byte("not a zip"))
				Expect(err).To(MatchError(ContainSubstring("invalid zip file")))
			})
		})

		When("given a real WhatsApp export zip", func() {
			It("extracts chat content from the actual file", func() {
				zipPath := "/Users/i547928/dev/private/friends-football/WhatsApp Chat - כדורגל חברים⚽👨🏽_🤝_👨🏻.zip"

				if _, err := os.Stat(zipPath); os.IsNotExist(err) {
					Skip("WhatsApp export zip not found")
				}

				content, err := parser.ExtractChatFromZip(zipPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(content).NotTo(BeEmpty())
				Expect(len(content)).To(BeNumerically(">", 1000))
			})
		})
	})

	Context("Chat content analysis", func() {
		It("sample chat contains expected registration patterns", func() {
			Expect(sampleChaoticChat).To(ContainSubstring("registration"))
			Expect(sampleChaoticChat).To(ContainSubstring("can't make it"))
			Expect(sampleChaoticChat).To(ContainSubstring("System:"))
		})

		It("expected players list has exactly 12 entries", func() {
			Expect(expectedPlayers).To(HaveLen(12))
		})
	})

	Context("Error constants", func() {
		It("defines expected error types", func() {
			Expect(parser.ErrNoAPIKey).To(MatchError("GEMINI_API_KEY environment variable not set"))
			Expect(parser.ErrInvalidResponse).To(MatchError("invalid response from Gemini"))
			Expect(parser.ErrPlayerCountInvalid).To(MatchError("expected exactly 12 players from chat"))
			Expect(parser.ErrChatFileNotFound).To(MatchError("_chat.txt not found in zip archive"))
			Expect(parser.ErrInvalidZip).To(MatchError("invalid zip file"))
		})
	})

	Context("Parser.New", func() {
		It("returns ErrNoAPIKey when GEMINI_API_KEY is not set", func() {
			// Save and clear existing key
			originalKey := os.Getenv("GEMINI_API_KEY")
			os.Unsetenv("GEMINI_API_KEY")
			defer func() {
				if originalKey != "" {
					os.Setenv("GEMINI_API_KEY", originalKey)
				}
			}()

			_, err := parser.New(context.Background())
			Expect(err).To(MatchError(parser.ErrNoAPIKey))
		})
	})

	// Integration tests - skipped unless GEMINI_API_KEY is set
	Context("ParseChat integration", func() {
		var p *parser.Parser
		var ctx context.Context

		BeforeEach(func() {
			if os.Getenv("GEMINI_API_KEY") == "" {
				Skip("GEMINI_API_KEY not set - skipping integration tests")
			}

			ctx = context.Background()
			var err error
			p, err = parser.New(ctx)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if p != nil {
				p.Close()
			}
		})

		It("parses sample chat and returns 12 players", func() {
			names, err := p.ParseChat(ctx, sampleChaoticChat)
			// May fail with rate limit - that's expected
			if err == parser.ErrAIOverloaded {
				Skip("AI overloaded - rate limit hit during test")
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(names).To(HaveLen(12))
		})
	})
})

// createTestZip creates an in-memory zip file with the given filename and content.
func createTestZip(filename, content string) []byte {
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	f, _ := w.Create(filename)
	f.Write([]byte(content))
	w.Close()

	return buf.Bytes()
}

// seedTestPlayers populates the database with test players including aliases.
func seedTestPlayers() {
	players := []struct {
		name    string
		phone   string
		aliases []string
	}{
		{"Omer", "+972501111111", []string{"Omeri", "Omer K"}},
		{"Dan", "+972502222222", []string{"Danny", "Daniel"}},
		{"Niv", "+972503333333", []string{"Nivi"}},
		{"Yossi", "+972504444444", []string{"Yosef", "Joe"}},
		{"Amit", "+972505555555", []string{"Amiti"}},
		{"Roi", "+972506666666", []string{"Roy"}},
		{"Gal", "+972507777777", []string{"Gali"}},
		{"Tomer", "+972508888888", []string{"Tom"}},
		{"Oren", "+972509999999", []string{"Oreni"}},
		{"Ben", "+972500000000", []string{"Benny", "Benjamin"}},
		{"Lior", "+972501010101", []string{"Li"}},
		{"Shai", "+972502020202", []string{"Shay"}},
		{"Noam", "+972503030303", []string{"Noami"}},
		{"Eyal", "+972504040404", []string{"Eyali"}},
	}

	for _, p := range players {
		aliasesJSON, _ := json.Marshal(p.aliases)
		database.DB.Exec(`
			INSERT INTO players (name, phone, nickname_aliases, base_skill_rating, base_fitness_rating)
			VALUES (?, ?, ?, 6.0, 2.0)
		`, p.name, p.phone, string(aliasesJSON))
	}
}
