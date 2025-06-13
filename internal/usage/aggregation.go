package usage

import "time"

func (u *UsageTracker) GetUsageReport() (UsageReport, error) {
	u.lock.RLock()
	defer u.lock.RUnlock()

	return UsageReport{}, nil
}

type UsageReport struct {
	Start    time.Time
	End      time.Time
	Projects []ProjectUsageReport
}

type ProjectUsageReport struct {
	Name       string
	Workspaces []WorkspaceUsageReport
}

type WorkspaceUsageReport struct {
	Name string
	MCPs []MCPUsageReport
}

type MCPUsageReport struct {
	Name  string
	Hours int
}
