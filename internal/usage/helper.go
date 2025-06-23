package usage

import "time"

type usagePerDay struct {
	date     time.Time
	duration time.Duration
}

const DAY = 24 * time.Hour

func calculateUsage(start time.Time, end time.Time) (result []usagePerDay) {
	start = start.UTC()
	end = end.UTC()
	duration := start.Sub(end).Abs()
	return _calculateUsage(start, end, duration)
}

// recursive function which calculates the usage per day in the time between current and end. Should not be used
// directly, only through the calculateUsage method.
func _calculateUsage(current time.Time, end time.Time, duration time.Duration) []usagePerDay {
	currentDate := current.Truncate(DAY)
	endDate := end.Truncate(DAY)
	if currentDate.Equal(endDate) {
		// its the same day, so we need to put the remaining duration onto the current day
		return []usagePerDay{{
			date:     current,
			duration: duration,
		}}

	}

	if end.Before(current) { // if end is smaller then start, we reverse it
		return _calculateUsage(end, current, duration)
	}

	usageForTheDay := DAY - (time.Duration(current.Hour()) * time.Hour)
	nextDay := currentDate.Add(DAY)

	return append(_calculateUsage(nextDay, end, duration-usageForTheDay),
		usagePerDay{
			date:     current,
			duration: usageForTheDay,
		},
	)
}
