package server

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"friends-football/internal/database"
)

var _ = Describe("Auth Handlers Unit Tests", func() {
	Describe("createSessionToken and validateSessionToken", func() {
		It("creates a token that can be validated", func() {
			token := createSessionToken(42)
			playerID, err := validateSessionToken(token)

			Expect(err).NotTo(HaveOccurred())
			Expect(playerID).To(Equal(42))
		})

		It("works with different player IDs", func() {
			for _, id := range []int{1, 100, 999, 12345} {
				token := createSessionToken(id)
				playerID, err := validateSessionToken(token)

				Expect(err).NotTo(HaveOccurred())
				Expect(playerID).To(Equal(id))
			}
		})
	})

	Describe("validateSessionToken", func() {
		When("token is invalid", func() {
			It("rejects non-base64 token", func() {
				_, err := validateSessionToken("not-base64!!!")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid token encoding"))
			})

			It("rejects token without separator", func() {
				_, err := validateSessionToken("dGVzdA==") // "test" in base64
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid token format"))
			})

			It("rejects tampered signature", func() {
				// Base64 of "1:12345.tampered"
				_, err := validateSessionToken("MToxMjM0NS50YW1wZXJlZA==")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid signature"))
			})

			It("rejects malformed data part", func() {
				// Create valid signature but bad data format
				data := "invalid_no_colon"
				sig := signData(data)
				token := data + "." + sig
				// Base64 encode
				encoded := make([]byte, 100)
				n := copy(encoded, token)
				_, err := validateSessionToken(string(encoded[:n]))
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("mapFitnessCategory", func() {
		When("category is valid", func() {
			It("maps 'Poor' to FitnessPoor", func() {
				val, err := mapFitnessCategory("Poor")
				Expect(err).NotTo(HaveOccurred())
				Expect(val).To(Equal(database.FitnessPoor))
			})

			It("maps 'Normal' to FitnessNormal", func() {
				val, err := mapFitnessCategory("Normal")
				Expect(err).NotTo(HaveOccurred())
				Expect(val).To(Equal(database.FitnessNormal))
			})

			It("maps 'Good' to FitnessGood", func() {
				val, err := mapFitnessCategory("Good")
				Expect(err).NotTo(HaveOccurred())
				Expect(val).To(Equal(database.FitnessGood))
			})

			It("is case-insensitive", func() {
				for _, category := range []string{"poor", "POOR", "Poor", "pOoR"} {
					val, err := mapFitnessCategory(category)
					Expect(err).NotTo(HaveOccurred())
					Expect(val).To(Equal(database.FitnessPoor))
				}
			})

			It("trims whitespace", func() {
				val, err := mapFitnessCategory("  Normal  ")
				Expect(err).NotTo(HaveOccurred())
				Expect(val).To(Equal(database.FitnessNormal))
			})
		})

		When("category is invalid", func() {
			It("returns error for unknown category", func() {
				_, err := mapFitnessCategory("Excellent")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Poor"))
				Expect(err.Error()).To(ContainSubstring("Normal"))
				Expect(err.Error()).To(ContainSubstring("Good"))
			})

			It("returns error for empty string", func() {
				_, err := mapFitnessCategory("")
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("mapFitnessToCategory", func() {
		It("maps FitnessPoor to 'Poor'", func() {
			Expect(mapFitnessToCategory(database.FitnessPoor)).To(Equal("Poor"))
		})

		It("maps FitnessNormal to 'Normal'", func() {
			Expect(mapFitnessToCategory(database.FitnessNormal)).To(Equal("Normal"))
		})

		It("maps FitnessGood to 'Good'", func() {
			Expect(mapFitnessToCategory(database.FitnessGood)).To(Equal("Good"))
		})

		It("defaults to 'Normal' for unknown values", func() {
			Expect(mapFitnessToCategory(0)).To(Equal("Normal"))
			Expect(mapFitnessToCategory(99)).To(Equal("Normal"))
		})
	})
})
