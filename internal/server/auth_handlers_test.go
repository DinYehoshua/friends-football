package server

import (
	"encoding/base64"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// createTestJWT creates a minimal JWT for testing (header.payload.signature)
// Note: signature is not valid, but we're testing claim validation, not crypto
func createTestJWT(claims GoogleClaims) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	signature := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
	return header + "." + payloadB64 + "." + signature
}

var _ = Describe("Auth Handlers Unit Tests", func() {
	Describe("verifyGoogleIDToken", func() {
		var originalClientID string

		BeforeEach(func() {
			// Save and set test client ID
			originalClientID = googleClientID
			googleClientID = "test-client-id.apps.googleusercontent.com"
		})

		AfterEach(func() {
			// Restore original client ID
			googleClientID = originalClientID
		})

		It("rejects when GOOGLE_CLIENT_ID is not configured", func() {
			googleClientID = ""
			token := createTestJWT(GoogleClaims{
				Email:         "test@example.com",
				EmailVerified: true,
				Name:          "Test User",
				Sub:           "123456",
				Aud:           "test-client-id.apps.googleusercontent.com",
				Iss:           "accounts.google.com",
				Exp:           time.Now().Add(1 * time.Hour).Unix(),
			})

			_, err := verifyGoogleIDToken(token)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("GOOGLE_CLIENT_ID not configured"))
		})

		It("rejects invalid token format (not 3 parts)", func() {
			_, err := verifyGoogleIDToken("invalid-token")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid token format"))
		})

		It("rejects invalid base64 payload", func() {
			_, err := verifyGoogleIDToken("header.!!!invalid-base64!!!.signature")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to decode payload"))
		})

		It("rejects invalid JSON payload", func() {
			invalidPayload := base64.RawURLEncoding.EncodeToString([]byte("not-json"))
			_, err := verifyGoogleIDToken("header." + invalidPayload + ".signature")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse claims"))
		})

		It("rejects wrong audience (confused deputy attack)", func() {
			token := createTestJWT(GoogleClaims{
				Email:         "attacker@example.com",
				EmailVerified: true,
				Name:          "Attacker",
				Sub:           "attacker-123",
				Aud:           "different-app-client-id.apps.googleusercontent.com", // Wrong client ID
				Iss:           "accounts.google.com",
				Exp:           time.Now().Add(1 * time.Hour).Unix(),
			})

			_, err := verifyGoogleIDToken(token)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid audience"))
		})

		It("rejects invalid issuer", func() {
			token := createTestJWT(GoogleClaims{
				Email:         "test@example.com",
				EmailVerified: true,
				Name:          "Test User",
				Sub:           "123456",
				Aud:           "test-client-id.apps.googleusercontent.com",
				Iss:           "malicious-issuer.com", // Wrong issuer
				Exp:           time.Now().Add(1 * time.Hour).Unix(),
			})

			_, err := verifyGoogleIDToken(token)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid issuer"))
		})

		It("rejects expired token", func() {
			token := createTestJWT(GoogleClaims{
				Email:         "test@example.com",
				EmailVerified: true,
				Name:          "Test User",
				Sub:           "123456",
				Aud:           "test-client-id.apps.googleusercontent.com",
				Iss:           "accounts.google.com",
				Exp:           time.Now().Add(-1 * time.Hour).Unix(), // Expired
			})

			_, err := verifyGoogleIDToken(token)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("token expired"))
		})

		It("rejects token without email", func() {
			token := createTestJWT(GoogleClaims{
				Email:         "", // No email
				EmailVerified: true,
				Name:          "Test User",
				Sub:           "123456",
				Aud:           "test-client-id.apps.googleusercontent.com",
				Iss:           "accounts.google.com",
				Exp:           time.Now().Add(1 * time.Hour).Unix(),
			})

			_, err := verifyGoogleIDToken(token)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no email in token"))
		})

		It("accepts valid token with accounts.google.com issuer", func() {
			token := createTestJWT(GoogleClaims{
				Email:         "test@example.com",
				EmailVerified: true,
				Name:          "Test User",
				Sub:           "123456",
				Aud:           "test-client-id.apps.googleusercontent.com",
				Iss:           "accounts.google.com",
				Exp:           time.Now().Add(1 * time.Hour).Unix(),
			})

			claims, err := verifyGoogleIDToken(token)
			Expect(err).NotTo(HaveOccurred())
			Expect(claims.Email).To(Equal("test@example.com"))
			Expect(claims.Name).To(Equal("Test User"))
		})

		It("accepts valid token with https://accounts.google.com issuer", func() {
			token := createTestJWT(GoogleClaims{
				Email:         "test@example.com",
				EmailVerified: true,
				Name:          "Test User",
				Sub:           "123456",
				Aud:           "test-client-id.apps.googleusercontent.com",
				Iss:           "https://accounts.google.com",
				Exp:           time.Now().Add(1 * time.Hour).Unix(),
			})

			claims, err := verifyGoogleIDToken(token)
			Expect(err).NotTo(HaveOccurred())
			Expect(claims.Email).To(Equal("test@example.com"))
		})
	})

	Describe("generateClaimToken", func() {
		It("generates unique tokens", func() {
			token1 := generateClaimToken()
			token2 := generateClaimToken()

			Expect(token1).NotTo(BeEmpty())
			Expect(token2).NotTo(BeEmpty())
			Expect(token1).NotTo(Equal(token2))
		})

		It("generates URL-safe tokens", func() {
			token := generateClaimToken()
			// Should be valid base64url
			_, err := base64.URLEncoding.DecodeString(token)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("cleanExpiredClaimTokens", func() {
		It("removes expired tokens", func() {
			expiredToken := "expired-token"
			validToken := "valid-token"

			claimTokens[expiredToken] = claimTokenData{
				Email:     "expired@example.com",
				Name:      "Expired",
				ExpiresAt: time.Now().Add(-1 * time.Minute),
			}
			claimTokens[validToken] = claimTokenData{
				Email:     "valid@example.com",
				Name:      "Valid",
				ExpiresAt: time.Now().Add(10 * time.Minute),
			}

			cleanExpiredClaimTokens()

			_, expiredExists := claimTokens[expiredToken]
			_, validExists := claimTokens[validToken]

			Expect(expiredExists).To(BeFalse())
			Expect(validExists).To(BeTrue())

			// Clean up
			delete(claimTokens, validToken)
		})
	})

	Describe("mapFitnessCategory", func() {
		It("maps all valid categories", func() {
			cases := []struct {
				input    string
				expected int
			}{
				{"Low", 1},
				{"low", 1},
				{"LOW", 1},
				{"Ok", 2},
				{"ok", 2},
				{"Good", 3},
				{"good", 3},
				{"Great", 4},
				{"great", 4},
				{"Excellent", 5},
				{"excellent", 5},
			}

			for _, tc := range cases {
				result, err := mapFitnessCategory(tc.input)
				Expect(err).NotTo(HaveOccurred(), "Input: %s", tc.input)
				Expect(result).To(Equal(tc.expected), "Input: %s", tc.input)
			}
		})

		It("handles whitespace", func() {
			result, err := mapFitnessCategory("  Good  ")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(3))
		})

		It("rejects invalid category", func() {
			_, err := mapFitnessCategory("Super")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Low"))
		})
	})

	Describe("mapFitnessToCategory", func() {
		It("maps all valid integers", func() {
			cases := []struct {
				input    int
				expected string
			}{
				{1, "Low"},
				{2, "Ok"},
				{3, "Good"},
				{4, "Great"},
				{5, "Excellent"},
			}

			for _, tc := range cases {
				result := mapFitnessToCategory(tc.input)
				Expect(result).To(Equal(tc.expected), "Input: %d", tc.input)
			}
		})

		It("defaults to Ok for unknown values", func() {
			result := mapFitnessToCategory(99)
			Expect(result).To(Equal("Ok"))
		})
	})

	Describe("session token functions", func() {
		It("creates and validates session token", func() {
			playerID := 42
			token := createSessionToken(playerID)

			Expect(token).NotTo(BeEmpty())

			validatedID, err := validateSessionToken(token)
			Expect(err).NotTo(HaveOccurred())
			Expect(validatedID).To(Equal(playerID))
		})

		It("rejects tampered token", func() {
			token := createSessionToken(42)
			// Tamper with the token
			tamperedToken := token[:len(token)-5] + "XXXXX"

			_, err := validateSessionToken(tamperedToken)
			Expect(err).To(HaveOccurred())
		})

		It("rejects invalid base64", func() {
			_, err := validateSessionToken("not-valid-base64!!!")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid token encoding"))
		})

		It("rejects token without signature", func() {
			data := base64.URLEncoding.EncodeToString([]byte("1:12345"))
			_, err := validateSessionToken(data)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid token format"))
		})
	})
})
