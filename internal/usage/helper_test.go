package usage

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Helper Module", func() {
	Context("Usage calculation", func() {
		It("should calculate the usage for a 1 week span", func() {
			firstDayDuration := 23*time.Hour + 59*time.Minute + 59*time.Second

			end := time.Now().UTC()
			endDate := end.Truncate(24 * time.Hour)
			end = endDate.Add(firstDayDuration)

			start := end.Add(-7 * 24 * time.Hour)
			start = start.Truncate(24 * time.Hour)

			result := calculateUsage(start, end)

			Ω(result).Should(HaveLen(8))

			Ω(result[0].Date.Time.Equal(endDate)).Should(BeTrue(), "first date must equal end date")
			Ω(result[0].Usage.Duration).Should(Equal(firstDayDuration), "first day must have the right duration")
			for _, usage := range result[1:] {
				Ω(usage.Usage.Duration).Should(Equal(24 * time.Hour))
			}

			reversed := calculateUsage(end, start)
			Ω(result).Should(Equal(reversed), "the calculation must be reversed the same")
		})
	})
})
