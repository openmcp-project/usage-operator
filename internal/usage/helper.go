package usage

import (
	"errors"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/google/uuid"

	v1 "github.com/openmcp-project/usage-operator/api/usage/v1"
)

const DAY = 24 * time.Hour

func limitUsage(val time.Duration, max time.Duration) time.Duration {
	if val > max {
		return max
	}

	return val
}

func calculateUsage(start time.Time, end time.Time) (result []v1.DailyUsage) {
	start = start.UTC()
	end = end.UTC()
	duration := start.Sub(end).Abs()
	return _calculateUsage(start, end, duration)
}

// recursive function which calculates the usage per day in the time between current and end. Should not be used
// directly, only through the calculateUsage method.
func _calculateUsage(current time.Time, end time.Time, duration time.Duration) []v1.DailyUsage {
	currentDate := current.Truncate(DAY)
	endDate := end.Truncate(DAY)
	if currentDate.Equal(endDate) {
		// its the same day, so we need to put the remaining duration onto the current day
		return []v1.DailyUsage{{
			Date:  metav1.NewTime(current),
			Usage: metav1.Duration{Duration: limitUsage(duration, DAY)},
		}}

	}

	if end.Before(current) { // if end is smaller then start, we reverse it
		return _calculateUsage(end, current, duration)
	}

	usageForTheDay := DAY - (time.Duration(current.Hour()) * time.Hour)
	nextDay := currentDate.Add(DAY)

	return append(_calculateUsage(nextDay, end, duration-usageForTheDay),
		v1.DailyUsage{
			Date:  metav1.NewTime(current),
			Usage: metav1.Duration{Duration: limitUsage(usageForTheDay, DAY)},
		},
	)
}

func GetNamespacedName(project, workspace string) string {
	return "project-" + project + "--ws-" + workspace
}

func GetObjectKey(project, workspace, mcp string) (client.ObjectKey, error) {
	name := GetNamespacedName(project, workspace) + "-" + mcp
	id := uuid.NewSHA1(uuid.Nil, []byte(name))

	if id.String() == "" {
		return client.ObjectKey{}, errors.New("can't generate uuid from input")
	}

	return client.ObjectKey{
		Name: id.String(),
	}, nil
}

// merges two DailyUsages where no Date is double
func MergeDailyUsages(a []v1.DailyUsage, b []v1.DailyUsage) []v1.DailyUsage {
	aggregatedUsage := make(map[string]metav1.Duration)

	// Helper function to add daily usage to the map
	addUsageToMap := func(du v1.DailyUsage) {
		dateKey := du.Date.Format("2006-01-02") // Format to YYYY-MM-DD string
		usage := aggregatedUsage[dateKey]
		usage.Duration += du.Usage.Duration
		aggregatedUsage[dateKey] = usage
	}

	// Iterate over the first slice and add/sum usage to the map
	for _, daily := range a {
		addUsageToMap(daily)
	}

	// Iterate over the second slice and add/sum usage to the map
	for _, daily := range b {
		addUsageToMap(daily)
	}

	mergedList := make([]v1.DailyUsage, 0, len(aggregatedUsage))
	for dateStr, totalUsage := range aggregatedUsage {
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		mergedList = append(mergedList, v1.DailyUsage{
			Date:  metav1.Time{Time: t.UTC()}, // Store as UTC for consistency
			Usage: metav1.Duration{Duration: limitUsage(totalUsage.Duration, DAY)},
		})
	}

	// Sort the resulting slice by date in ascending order
	sort.Slice(mergedList, func(i, j int) bool {
		return mergedList[i].Date.Before(&mergedList[j].Date)
	})

	return mergedList
}
