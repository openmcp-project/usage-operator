package usage

import (
	"fmt"
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

			Ω(len(result)).Should(Equal(8))

			Ω(result[0].date.Equal(endDate)).Should(Equal(true), "first date must equal end date")
			Ω(result[0].duration).Should(Equal(firstDayDuration), "first day must have the right duration")
			for _, usage := range result[1:] {
				Ω(usage.duration).Should(Equal(24 * time.Hour))
			}
		})

		It("reversed: should calculate the usage for a 1 week span", func() {
			firstDayDuration := 23*time.Hour + 59*time.Minute + 59*time.Second

			end := time.Now().UTC()
			endDate := end.Truncate(24 * time.Hour)
			end = endDate.Add(firstDayDuration)

			start := end.Add(-7 * 24 * time.Hour)
			start = start.Truncate(24 * time.Hour)

			result := calculateUsage(end, start)

			Ω(len(result)).Should(Equal(8))

			Ω(result[0].date.Equal(endDate)).Should(Equal(true), "first date must equal end date")
			Ω(result[0].duration).Should(Equal(firstDayDuration), "first day must have the right duration")
			for _, usage := range result[1:] {
				fmt.Println(usage.date, usage.duration)
				Ω(usage.duration).Should(Equal(24 * time.Hour))
			}
		})
	})
})
