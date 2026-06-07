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
			It("maps 'Low' to FitnessLow", func() {
				val, err := mapFitnessCategory("Low")
				Expect(err).NotTo(HaveOccurred())
				Expect(val).To(Equal(database.FitnessLow))
			})

			It("maps 'Poor' to FitnessPoor", func() {
				val, err := mapFitnessCategory("Poor")
				Expect(err).NotTo(HaveOccurred())
				Expect(val).To(Equal(database.FitnessPoor))
			})

			It("maps 'Average' to FitnessAverage", func() {
				val, err := mapFitnessCategory("Average")
				Expect(err).NotTo(HaveOccurred())
				Expect(val).To(Equal(database.FitnessAverage))
			})

			It("maps 'Good' to FitnessGood", func() {
				val, err := mapFitnessCategory("Good")
				Expect(err).NotTo(HaveOccurred())
				Expect(val).To(Equal(database.FitnessGood))
			})

			It("maps 'Excellent' to FitnessExcellent", func() {
				val, err := mapFitnessCategory("Excellent")
				Expect(err).NotTo(HaveOccurred())
				Expect(val).To(Equal(database.FitnessExcellent))
			})

			It("is case-insensitive", func() {
				for _, category := range []string{"low", "LOW", "Low", "lOw"} {
					val, err := mapFitnessCategory(category)
					Expect(err).NotTo(HaveOccurred())
					Expect(val).To(Equal(database.FitnessLow))
				}
			})

			It("trims whitespace", func() {
				val, err := mapFitnessCategory("  Average  ")
				Expect(err).NotTo(HaveOccurred())
				Expect(val).To(Equal(database.FitnessAverage))
			})
		})

		When("category is invalid", func() {
			It("returns error for unknown category", func() {
				_, err := mapFitnessCategory("Super")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Low"))
				Expect(err.Error()).To(ContainSubstring("Excellent"))
			})

			It("returns error for empty string", func() {
				_, err := mapFitnessCategory("")
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("mapFitnessToCategory", func() {
		It("maps FitnessLow to 'Low'", func() {
			Expect(mapFitnessToCategory(database.FitnessLow)).To(Equal("Low"))
		})

		It("maps FitnessPoor to 'Poor'", func() {
			Expect(mapFitnessToCategory(database.FitnessPoor)).To(Equal("Poor"))
		})

		It("maps FitnessAverage to 'Average'", func() {
			Expect(mapFitnessToCategory(database.FitnessAverage)).To(Equal("Average"))
		})

		It("maps FitnessGood to 'Good'", func() {
			Expect(mapFitnessToCategory(database.FitnessGood)).To(Equal("Good"))
		})

		It("maps FitnessExcellent to 'Excellent'", func() {
			Expect(mapFitnessToCategory(database.FitnessExcellent)).To(Equal("Excellent"))
		})

		It("defaults to 'Average' for unknown values", func() {
			Expect(mapFitnessToCategory(0)).To(Equal("Average"))
			Expect(mapFitnessToCategory(99)).To(Equal("Average"))
		})
	})
})
