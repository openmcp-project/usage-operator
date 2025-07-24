package usage

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/openmcp-project/usage-operator/api/usage/v1"
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

			Expect(result).Should(HaveLen(8))

			Expect(result[0].Date.Time.Equal(endDate)).Should(BeTrue(), "first date must equal end date")
			Expect(result[0].Usage.Duration).Should(Equal(firstDayDuration), "first day must have the right duration")
			for _, usage := range result[1:] {
				Expect(usage.Usage.Duration).Should(Equal(24 * time.Hour))
			}

			reversed := calculateUsage(end, start)
			Expect(result).Should(Equal(reversed), "the calculation must be reversed the same")
		})

		It("should merge dailyusage", func() {
			dailyUsage1 := []v1.DailyUsage{
				{
					Date: metav1.NewTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
					Usage: metav1.Duration{
						Duration: 4 * time.Hour,
					},
				},
				{
					Date: metav1.NewTime(time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)),
					Usage: metav1.Duration{
						Duration: 4 * time.Hour,
					},
				},
			}

			dailyUsage2 := []v1.DailyUsage{
				{
					Date: metav1.NewTime(time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)),
					Usage: metav1.Duration{
						Duration: 20 * time.Hour,
					},
				},
				{
					Date: metav1.NewTime(time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC)),
					Usage: metav1.Duration{
						Duration: 8 * time.Hour,
					},
				},
			}

			mergedUsages := MergeDailyUsages(dailyUsage1, dailyUsage2)

			Expect(mergedUsages).Should(HaveLen(3))

			Expect(mergedUsages[1].Date).Should(Equal(metav1.NewTime(time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC))))
			Expect(mergedUsages[1].Usage.Hours()).Should(Equal(24.0))
		})
	})
	Context("ObjectKey Generation", func() {
		It("should generate the same objectkey with the same input", func() {
			project := "Testproject"
			workspace := "Testworkspace"
			mcp := "Test"

			key1, err := GetObjectKey(project, workspace, mcp)
			Expect(err).ShouldNot(HaveOccurred())
			key2, err := GetObjectKey(project, workspace, mcp)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(key1.Name).Should(Equal(key2.Name))
		})
	})
})
