package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"

	"github.com/openmcp-project/usage-operator/api/v1alpha1"
)

// loadUsage reads a ResourceUsage from a YAML file in the testdata directory.
func loadUsage(t *testing.T, name string) *v1alpha1.ResourceUsage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}
	var u v1alpha1.ResourceUsage
	if err := yaml.Unmarshal(data, &u); err != nil {
		t.Fatalf("unmarshal testdata/%s: %v", name, err)
	}
	return &u
}

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return v
}

func mustDuration(t *testing.T, s string) time.Duration {
	t.Helper()
	d, err := time.ParseDuration(s)
	if err != nil {
		t.Fatalf("parse duration %q: %v", s, err)
	}
	return d
}

// jsonValue wraps a plain Go value as apiextensionsv1.JSON.
func jsonValue(t *testing.T, v any) apiextensionsv1.JSON {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json value: %v", err)
	}
	return apiextensionsv1.JSON{Raw: raw}
}

// --- ComputeUsageDuration ---

func TestComputeUsageDuration(t *testing.T) {
	cases := []struct {
		name          string
		fixtures      []string
		start, end    string
		wantUsed      string
		wantRemainder string
	}{
		{
			name:          "full tracking — query matches tracking period exactly",
			fixtures:      []string{"full_tracking.yaml"},
			start:         "2026-07-01T10:00:00Z",
			end:           "2026-07-01T12:00:00Z",
			wantUsed:      "2h",
			wantRemainder: "0",
		},
		{
			name:          "full tracking — query window narrower than tracking period",
			fixtures:      []string{"full_tracking.yaml"},
			start:         "2026-07-01T10:30:00Z",
			end:           "2026-07-01T11:30:00Z",
			wantUsed:      "1h",
			wantRemainder: "0",
		},
		{
			name:          "partial tracking — 30m untracked at start of window",
			fixtures:      []string{"partial_tracking.yaml"},
			start:         "2026-07-01T10:00:00Z",
			end:           "2026-07-01T12:00:00Z",
			wantUsed:      "1h30m",
			wantRemainder: "0",
		},
		{
			name:          "fragmented tracking — three separate intervals totalling 2h15m",
			fixtures:      []string{"fragmented_tracking.yaml"},
			start:         "2026-07-01T09:00:00Z",
			end:           "2026-07-01T12:00:00Z",
			wantUsed:      "2h15m",
			wantRemainder: "0",
		},
		{
			name:          "overlapping intervals — merged to 2h30m",
			fixtures:      []string{"overlapping_intervals.yaml"},
			start:         "2026-07-01T10:00:00Z",
			end:           "2026-07-01T13:00:00Z",
			wantUsed:      "2h30m",
			wantRemainder: "0",
		},
		{
			// day1: fully tracked (24h), day2: 12h tracked (06:00-18:00).
			// Both tracking periods together cover the full 48h window → remainder=0.
			// The 12h gap on day2 is within its tracking period, not a remainder.
			name:          "two consecutive day objects — query spans both",
			fixtures:      []string{"day1.yaml", "day2.yaml"},
			start:         "2026-07-01T00:00:00Z",
			end:           "2026-07-03T00:00:00Z",
			wantUsed:      "36h",
			wantRemainder: "0",
		},
		{
			// day1 contributes 12h (12:00-24:00), day2 contributes 6h (06:00-12:00)
			name:          "two consecutive day objects — query clips both tracking periods",
			fixtures:      []string{"day1.yaml", "day2.yaml"},
			start:         "2026-07-01T12:00:00Z",
			end:           "2026-07-02T12:00:00Z",
			wantUsed:      "18h",
			wantRemainder: "0",
		},
		{
			name:          "query window outside all tracking periods",
			fixtures:      []string{"full_tracking.yaml"},
			start:         "2026-07-02T00:00:00Z",
			end:           "2026-07-02T02:00:00Z",
			wantUsed:      "0",
			wantRemainder: "2h",
		},
		{
			// full_tracking covers 10:00-12:00. Query is 09:00-13:00 → 2h remainder (uncovered).
			name:          "query window wider than tracking period — uncovered portion is remainder",
			fixtures:      []string{"full_tracking.yaml"},
			start:         "2026-07-01T09:00:00Z",
			end:           "2026-07-01T13:00:00Z",
			wantUsed:      "2h",
			wantRemainder: "2h",
		},
		{
			name:          "equal start and end — zero window",
			fixtures:      []string{"full_tracking.yaml"},
			start:         "2026-07-01T10:00:00Z",
			end:           "2026-07-01T10:00:00Z",
			wantUsed:      "0",
			wantRemainder: "0",
		},
		{
			name:          "no usages passed",
			fixtures:      nil,
			start:         "2026-07-01T10:00:00Z",
			end:           "2026-07-01T12:00:00Z",
			wantUsed:      "0",
			wantRemainder: "2h",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usages := make([]*v1alpha1.ResourceUsage, len(tc.fixtures))
			for i, f := range tc.fixtures {
				usages[i] = loadUsage(t, f)
			}
			start := mustParse(t, tc.start)
			end := mustParse(t, tc.end)
			wantUsed := mustDuration(t, tc.wantUsed)
			wantRemainder := mustDuration(t, tc.wantRemainder)

			gotUsed, gotRemainder, err := ComputeUsageDuration(start, end, usages...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotUsed != wantUsed {
				t.Errorf("used: got %v, want %v", gotUsed, wantUsed)
			}
			if gotRemainder != wantRemainder {
				t.Errorf("remainder: got %v, want %v", gotRemainder, wantRemainder)
			}
		})
	}
}

func TestComputeUsageDuration_Errors(t *testing.T) {
	full := loadUsage(t, "full_tracking.yaml")

	t.Run("end before start", func(t *testing.T) {
		_, _, err := ComputeUsageDuration(
			mustParse(t, "2026-07-01T12:00:00Z"),
			mustParse(t, "2026-07-01T10:00:00Z"),
			full,
		)
		if err == nil {
			t.Fatal("expected error for end < start, got nil")
		}
	})

	t.Run("tracking period missing end", func(t *testing.T) {
		u := loadUsage(t, "full_tracking.yaml")
		u.Spec.TrackingPeriod.End = nil
		_, _, err := ComputeUsageDuration(
			mustParse(t, "2026-07-01T10:00:00Z"),
			mustParse(t, "2026-07-01T12:00:00Z"),
			u,
		)
		if err == nil {
			t.Fatal("expected error for missing tracking period end, got nil")
		}
	})

	t.Run("tracking period start equals end", func(t *testing.T) {
		u := loadUsage(t, "full_tracking.yaml")
		*u.Spec.TrackingPeriod.End = *u.Spec.TrackingPeriod.Start
		_, _, err := ComputeUsageDuration(
			mustParse(t, "2026-07-01T10:00:00Z"),
			mustParse(t, "2026-07-01T12:00:00Z"),
			u,
		)
		if err == nil {
			t.Fatal("expected error for tracking period start == end, got nil")
		}
	})
}

// --- ComputeTraitUsageDuration ---

func TestComputeTraitUsageDuration(t *testing.T) {
	cases := []struct {
		name          string
		fixtures      []string
		start, end    string
		traitName     string
		traitValue    any
		wantUsed      string
		wantRemainder string
	}{
		{
			name:          "full tracking — flavor=premium for entire window",
			fixtures:      []string{"full_tracking.yaml"},
			start:         "2026-07-01T10:00:00Z",
			end:           "2026-07-01T12:00:00Z",
			traitName:     "flavor",
			traitValue:    "premium",
			wantUsed:      "2h",
			wantRemainder: "0",
		},
		{
			name:          "full tracking — project=foo for first hour only",
			fixtures:      []string{"full_tracking.yaml"},
			start:         "2026-07-01T10:00:00Z",
			end:           "2026-07-01T12:00:00Z",
			traitName:     "project",
			traitValue:    "foo",
			wantUsed:      "1h",
			wantRemainder: "0",
		},
		{
			name:          "full tracking — project=bar for second hour only",
			fixtures:      []string{"full_tracking.yaml"},
			start:         "2026-07-01T10:00:00Z",
			end:           "2026-07-01T12:00:00Z",
			traitName:     "project",
			traitValue:    "bar",
			wantUsed:      "1h",
			wantRemainder: "0",
		},
		{
			// premium: 09:45-10:30 (45m) + 11:00-12:00 (60m) = 1h45m
			name:          "fragmented tracking — flavor=premium across two intervals",
			fixtures:      []string{"fragmented_tracking.yaml"},
			start:         "2026-07-01T09:00:00Z",
			end:           "2026-07-01T12:00:00Z",
			traitName:     "flavor",
			traitValue:    "premium",
			wantUsed:      "1h45m",
			wantRemainder: "0",
		},
		{
			name:          "fragmented tracking — flavor=standard for 30m",
			fixtures:      []string{"fragmented_tracking.yaml"},
			start:         "2026-07-01T09:00:00Z",
			end:           "2026-07-01T12:00:00Z",
			traitName:     "flavor",
			traitValue:    "standard",
			wantUsed:      "30m",
			wantRemainder: "0",
		},
		{
			// The trait exists in the object but the queried value is not present.
			name:          "trait present but value not found — zero used",
			fixtures:      []string{"full_tracking.yaml"},
			start:         "2026-07-01T10:00:00Z",
			end:           "2026-07-01T12:00:00Z",
			traitName:     "flavor",
			traitValue:    "standard",
			wantUsed:      "0",
			wantRemainder: "0",
		},
		{
			// The trait does not exist in any object — entire window is remainder.
			name:          "trait not present in any object",
			fixtures:      []string{"full_tracking.yaml"},
			start:         "2026-07-01T10:00:00Z",
			end:           "2026-07-01T12:00:00Z",
			traitName:     "unknown-trait",
			traitValue:    "any",
			wantUsed:      "0",
			wantRemainder: "0",
		},
		{
			// day1: flavor=premium 24h, day2: flavor=premium 6h (06:00-12:00).
			// Both tracking periods cover the full 48h → remainder=0.
			name:          "two objects — flavor=premium aggregated across both days",
			fixtures:      []string{"day1.yaml", "day2.yaml"},
			start:         "2026-07-01T00:00:00Z",
			end:           "2026-07-03T00:00:00Z",
			traitName:     "flavor",
			traitValue:    "premium",
			wantUsed:      "30h",
			wantRemainder: "0",
		},
		{
			name:          "two objects — flavor=standard only on day2",
			fixtures:      []string{"day1.yaml", "day2.yaml"},
			start:         "2026-07-01T00:00:00Z",
			end:           "2026-07-03T00:00:00Z",
			traitName:     "flavor",
			traitValue:    "standard",
			wantUsed:      "6h",
			wantRemainder: "0",
		},
		{
			name:          "traitValue as raw JSON bytes",
			fixtures:      []string{"full_tracking.yaml"},
			start:         "2026-07-01T10:00:00Z",
			end:           "2026-07-01T12:00:00Z",
			traitName:     "flavor",
			traitValue:    []byte(`"premium"`),
			wantUsed:      "2h",
			wantRemainder: "0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usages := make([]*v1alpha1.ResourceUsage, len(tc.fixtures))
			for i, f := range tc.fixtures {
				usages[i] = loadUsage(t, f)
			}
			start := mustParse(t, tc.start)
			end := mustParse(t, tc.end)
			wantUsed := mustDuration(t, tc.wantUsed)
			wantRemainder := mustDuration(t, tc.wantRemainder)

			gotUsed, gotRemainder, err := ComputeTraitUsageDuration(start, end, tc.traitName, tc.traitValue, usages...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotUsed != wantUsed {
				t.Errorf("used: got %v, want %v", gotUsed, wantUsed)
			}
			if gotRemainder != wantRemainder {
				t.Errorf("remainder: got %v, want %v", gotRemainder, wantRemainder)
			}
		})
	}
}

// --- ComputeUsageDurationWithTraits ---

func TestComputeUsageDurationWithTraits(t *testing.T) {
	t.Run("full tracking — all trait durations correct", func(t *testing.T) {
		u := loadUsage(t, "full_tracking.yaml")
		start := mustParse(t, "2026-07-01T10:00:00Z")
		end := mustParse(t, "2026-07-01T12:00:00Z")

		gotUsed, gotRemainder, traits, err := ComputeUsageDurationWithTraits(start, end, u)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotUsed != 2*time.Hour {
			t.Errorf("used: got %v, want 2h", gotUsed)
		}
		if gotRemainder != 0 {
			t.Errorf("remainder: got %v, want 0", gotRemainder)
		}
		checkTraitDuration(t, traits, "flavor", "premium", 2*time.Hour)
		checkTraitDuration(t, traits, "project", "foo", 1*time.Hour)
		checkTraitDuration(t, traits, "project", "bar", 1*time.Hour)
	})

	t.Run("fragmented tracking — trait durations match intervals", func(t *testing.T) {
		u := loadUsage(t, "fragmented_tracking.yaml")
		start := mustParse(t, "2026-07-01T09:00:00Z")
		end := mustParse(t, "2026-07-01T12:00:00Z")

		gotUsed, gotRemainder, traits, err := ComputeUsageDurationWithTraits(start, end, u)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotUsed != mustDuration(t, "2h15m") {
			t.Errorf("used: got %v, want 2h15m", gotUsed)
		}
		if gotRemainder != 0 {
			t.Errorf("remainder: got %v, want 0", gotRemainder)
		}
		checkTraitDuration(t, traits, "flavor", "premium", mustDuration(t, "1h45m"))
		checkTraitDuration(t, traits, "flavor", "standard", 30*time.Minute)
	})

	t.Run("two consecutive day objects — traits aggregated", func(t *testing.T) {
		day1 := loadUsage(t, "day1.yaml")
		day2 := loadUsage(t, "day2.yaml")
		start := mustParse(t, "2026-07-01T00:00:00Z")
		end := mustParse(t, "2026-07-03T00:00:00Z")

		// Both tracking periods cover the full 48h window → remainder=0.
		gotUsed, gotRemainder, traits, err := ComputeUsageDurationWithTraits(start, end, day1, day2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotUsed != 36*time.Hour {
			t.Errorf("used: got %v, want 36h", gotUsed)
		}
		if gotRemainder != 0 {
			t.Errorf("remainder: got %v, want 0", gotRemainder)
		}
		checkTraitDuration(t, traits, "flavor", "premium", 30*time.Hour)
		checkTraitDuration(t, traits, "flavor", "standard", 6*time.Hour)
		checkTraitDuration(t, traits, "project", "alpha", 36*time.Hour)
	})

	t.Run("query window wider than tracking periods — remainder is the uncovered gap", func(t *testing.T) {
		// full_tracking covers 10:00-12:00. Query is 09:00-13:00 → 1h remainder on each side.
		u := loadUsage(t, "full_tracking.yaml")
		start := mustParse(t, "2026-07-01T09:00:00Z")
		end := mustParse(t, "2026-07-01T13:00:00Z")

		gotUsed, gotRemainder, traits, err := ComputeUsageDurationWithTraits(start, end, u)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotUsed != 2*time.Hour {
			t.Errorf("used: got %v, want 2h", gotUsed)
		}
		if gotRemainder != 2*time.Hour {
			t.Errorf("remainder: got %v, want 2h", gotRemainder)
		}
		checkTraitDuration(t, traits, "flavor", "premium", 2*time.Hour)
	})

	t.Run("overlapping intervals — trait duration deduplicated", func(t *testing.T) {
		u := loadUsage(t, "overlapping_intervals.yaml")
		start := mustParse(t, "2026-07-01T10:00:00Z")
		end := mustParse(t, "2026-07-01T13:00:00Z")

		gotUsed, _, traits, err := ComputeUsageDurationWithTraits(start, end, u)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotUsed != mustDuration(t, "2h30m") {
			t.Errorf("used: got %v, want 2h30m", gotUsed)
		}
		checkTraitDuration(t, traits, "flavor", "premium", mustDuration(t, "2h30m"))
	})
}

// --- TraitValueDurations.GetDurationForValue ---

func TestGetDurationForValue(t *testing.T) {
	tvds := TraitValueDurations{
		{Value: jsonValue(t, "premium"), Duration: 2 * time.Hour},
		{Value: jsonValue(t, "standard"), Duration: 30 * time.Minute},
	}

	cases := []struct {
		name  string
		value any
		want  time.Duration
	}{
		{"string match — premium", "premium", 2 * time.Hour},
		{"string match — standard", "standard", 30 * time.Minute},
		{"raw JSON bytes match", []byte(`"premium"`), 2 * time.Hour},
		{"not found returns zero", "unknown", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tvds.GetDurationForValue(tc.value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// --- TraitValueDurations.GetDominantTraitValue ---

func TestGetDominantTraitValue(t *testing.T) {
	premium := &TraitValueDuration{Value: jsonValue(t, "premium"), Duration: 3 * time.Hour}
	standard := &TraitValueDuration{Value: jsonValue(t, "standard"), Duration: 1 * time.Hour}
	null := &TraitValueDuration{Value: jsonValue(t, nil), Duration: 5 * time.Hour}

	cases := []struct {
		name        string
		tvds        TraitValueDurations
		includeNull bool
		want        *TraitValueDuration
	}{
		{
			name:        "returns entry with longest duration",
			tvds:        TraitValueDurations{standard, premium},
			includeNull: false,
			want:        premium,
		},
		{
			name:        "tie returns first entry",
			tvds:        TraitValueDurations{standard, {Value: jsonValue(t, "basic"), Duration: 1 * time.Hour}},
			includeNull: false,
			want:        standard,
		},
		{
			name:        "single entry is dominant",
			tvds:        TraitValueDurations{standard},
			includeNull: false,
			want:        standard,
		},
		{
			name:        "empty slice returns nil",
			tvds:        TraitValueDurations{},
			includeNull: false,
			want:        nil,
		},
		{
			name:        "nil slice returns nil",
			tvds:        nil,
			includeNull: false,
			want:        nil,
		},
		{
			name:        "null excluded when includeNull is false",
			tvds:        TraitValueDurations{null, standard},
			includeNull: false,
			want:        standard,
		},
		{
			name:        "null wins when includeNull is true",
			tvds:        TraitValueDurations{null, premium},
			includeNull: true,
			want:        null,
		},
		{
			name:        "all null with includeNull false returns nil",
			tvds:        TraitValueDurations{null},
			includeNull: false,
			want:        nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.tvds.GetDominantTraitValue(tc.includeNull)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// --- TraitValueDuration.ValueAs ---

func TestTraitValueDuration_ValueAs(t *testing.T) {
	type payload struct {
		Tier string `json:"tier"`
	}

	tvd := &TraitValueDuration{Value: jsonValue(t, payload{Tier: "premium"}), Duration: time.Hour}

	var got payload
	if err := tvd.ValueAs(&got); err != nil {
		t.Fatalf("ValueAs: %v", err)
	}
	if got.Tier != "premium" {
		t.Errorf("got tier %q, want %q", got.Tier, "premium")
	}
}

// checkTraitDuration asserts that traits[traitName] contains an entry for traitValue with the expected duration.
func checkTraitDuration(t *testing.T, traits map[string]TraitValueDurations, traitName, traitValue string, want time.Duration) {
	t.Helper()
	entries, ok := traits[traitName]
	if !ok {
		t.Errorf("trait %q not found in result", traitName)
		return
	}
	got, err := entries.GetDurationForValue(traitValue)
	if err != nil {
		t.Fatalf("GetDurationForValue(%q): %v", traitValue, err)
	}
	if got != want {
		t.Errorf("trait %q value %q: got %v, want %v", traitName, traitValue, got, want)
	}
}
