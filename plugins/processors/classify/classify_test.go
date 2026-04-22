package classify

import (
	"fmt"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"

	"github.com/stretchr/testify/require"
)

// runClassifyTest is a test helper that runs Init→Start→Add(metrics)→Stop
// and returns the resulting accumulator. Failures are reported via t.
func runClassifyTest(t *testing.T, cl *Classify, metrics []telegraf.Metric, waitTime ...time.Duration) *testutil.Accumulator {
	t.Helper()
	acc := &testutil.Accumulator{}
	require.NoError(t, cl.Init())
	require.NoError(t, cl.Start(acc))
	for _, m := range metrics {
		require.NoError(t, cl.Add(m, acc))
	}
	if len(waitTime) > 0 {
		time.Sleep(waitTime[0])
	}
	cl.Stop()
	return acc
}

// TestParseFullConfig is a basic smoke test: can the plugin start and classify
// a metric end-to-end with a full, valid configuration?
func TestParseFullConfig(t *testing.T) {
	cl := &Classify{
		SelectorTag:     "host",
		SelectorMapping: []map[string]string{{`pg\d{3}`: "database"}},
		MatchField:      "message",
		ResultTag:       "severity",
		MappedSelectorRegexes: map[string][]map[string]interface{}{
			"database": {
				{"ignore": "IGNORE"},
				{"okay": "OK"},
				{"warning": "WARNING"},
				{"critical": "CRITICAL"},
				{"unknown": ".*"},
			},
		},
	}
	if testing.Verbose() {
		cl.Log = testutil.Logger{}
	}

	m := metric.New("datapoint",
		map[string]string{"host": "pg123"},
		map[string]interface{}{"message": "WARNING:  badness happened"},
		time.Now())

	acc := runClassifyTest(t, cl, []telegraf.Metric{m})
	require.Len(t, acc.GetTelegrafMetrics(), 1)

	got := acc.GetTelegrafMetrics()[0]
	resultTag, ok := got.GetTag(cl.ResultTag)
	require.Truef(t, ok, "result tag %q not found in output metric", cl.ResultTag)
	require.Equal(t, "warning", resultTag)
}

// TestParseSelectorItem verifies all valid and invalid combinations of the
// selector_tag / selector_field options.
func TestParseSelectorItem(t *testing.T) {
	msr := map[string][]map[string]interface{}{
		"database": {
			{"ignore": "IGNORE"}, {"okay": "OK"}, {"warning": "WARNING"},
			{"critical": "CRITICAL"}, {"unknown": ".*"},
		},
	}
	sm := []map[string]string{{`pg\d{3}`: "database"}}

	tests := []struct {
		name          string
		selectorTag   string
		selectorField string
		wantErr       bool
	}{
		{name: "no selector"},
		{name: "only selector_tag", selectorTag: "host_tag"},
		{name: "both selector_tag and selector_field", selectorTag: "host_tag", selectorField: "host_field", wantErr: true},
		{name: "only selector_field", selectorField: "host_field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := &Classify{
				SelectorTag:           tt.selectorTag,
				SelectorField:         tt.selectorField,
				SelectorMapping:       sm,
				MatchField:            "message",
				ResultTag:             "severity",
				MappedSelectorRegexes: msr,
			}
			if testing.Verbose() {
				cl.Log = testutil.Logger{}
			}
			if tt.wantErr {
				require.Error(t, cl.Init())
				return
			}
			acc := &testutil.Accumulator{}
			require.NoError(t, cl.Init())
			require.NoError(t, cl.Start(acc))
			cl.Stop()
		})
	}
}

// TestParseMatchItem verifies all valid and invalid combinations of the
// match_tag / match_field options.
func TestParseMatchItem(t *testing.T) {
	msr := map[string][]map[string]interface{}{
		"database": {
			{"ignore": "IGNORE"}, {"okay": "OK"}, {"warning": "WARNING"},
			{"critical": "CRITICAL"}, {"unknown": ".*"},
		},
	}
	sm := []map[string]string{{`pg\d{3}`: "database"}}

	tests := []struct {
		name       string
		matchTag   string
		matchField string
		wantErr    bool
	}{
		{name: "no match tag or field", wantErr: true},
		{name: "only match_tag", matchTag: "message_tag"},
		{name: "both match_tag and match_field", matchTag: "message_tag", matchField: "message_field", wantErr: true},
		{name: "only match_field", matchField: "message_field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := &Classify{
				SelectorMapping:       sm,
				MatchTag:              tt.matchTag,
				MatchField:            tt.matchField,
				ResultTag:             "severity",
				MappedSelectorRegexes: msr,
			}
			if testing.Verbose() {
				cl.Log = testutil.Logger{}
			}
			if tt.wantErr {
				require.Error(t, cl.Init())
				return
			}
			acc := &testutil.Accumulator{}
			require.NoError(t, cl.Init())
			require.NoError(t, cl.Start(acc))
			cl.Stop()
		})
	}
}

// TestParseResultItem verifies all valid and invalid combinations of the
// result_tag / result_field options.
func TestParseResultItem(t *testing.T) {
	msr := map[string][]map[string]interface{}{
		"database": {
			{"ignore": "IGNORE"}, {"okay": "OK"}, {"warning": "WARNING"},
			{"critical": "CRITICAL"}, {"unknown": ".*"},
		},
	}
	sm := []map[string]string{{`pg\d{3}`: "database"}}

	tests := []struct {
		name        string
		resultTag   string
		resultField string
		wantErr     bool
	}{
		{name: "no result tag or field", wantErr: true},
		{name: "only result_tag", resultTag: "severity_tag"},
		{name: "both result_tag and result_field", resultTag: "severity_tag", resultField: "severity_field", wantErr: true},
		{name: "only result_field", resultField: "severity_field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := &Classify{
				SelectorMapping:       sm,
				MatchField:            "message",
				ResultTag:             tt.resultTag,
				ResultField:           tt.resultField,
				MappedSelectorRegexes: msr,
			}
			if testing.Verbose() {
				cl.Log = testutil.Logger{}
			}
			if tt.wantErr {
				require.Error(t, cl.Init())
				return
			}
			acc := &testutil.Accumulator{}
			require.NoError(t, cl.Init())
			require.NoError(t, cl.Start(acc))
			cl.Stop()
		})
	}
}

// TestReturnSampleConfig verifies that SampleConfig returns non-empty content.
func TestReturnSampleConfig(t *testing.T) {
	cl := &Classify{}
	require.NotEmpty(t, cl.SampleConfig(), "SampleConfig must return non-empty content")
}

// TestBadSelectorRegex verifies that invalid selector_mapping regexes are rejected.
func TestBadSelectorRegex(t *testing.T) {
	msr := map[string][]map[string]interface{}{"database": {{"ignore": "IGNORE"}}}

	tests := []struct {
		name    string
		regex   string
		wantErr bool
	}{
		{name: "valid selector regex", regex: `pg\d{3}`},
		{name: "bad selector regex", regex: `*pg\d{3}`, wantErr: true},
		{name: "empty selector regex", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := &Classify{
				SelectorTag:           "host",
				SelectorMapping:       []map[string]string{{tt.regex: "database"}},
				MatchField:            "message",
				ResultTag:             "severity",
				MappedSelectorRegexes: msr,
			}
			if testing.Verbose() {
				cl.Log = testutil.Logger{}
			}
			if tt.wantErr {
				require.Error(t, cl.Init())
			} else {
				require.NoError(t, cl.Init())
			}
		})
	}
}

// TestBadCategoryRegexType verifies that all supported and unsupported value
// types for mapped_selector_regexes category entries are handled correctly.
func TestBadCategoryRegexType(t *testing.T) {
	myString := "IGNORE"

	tests := []struct {
		name    string
		value   interface{}
		wantErr bool
	}{
		{name: "single string regex", value: "IGNORE"},
		{name: "multi-line string regex", value: "    IGNORE\n    DO NOT CARE\n    FUGGEDDABOUDIT\n    "},
		{name: "multi-line string with leading whitespace", value: "  \n    IGNORE\n    DO NOT CARE\n    "},
		{name: "multi-line string with no regexes", value: "    "},
		{name: "array of strings regex", value: []string{"IGNORE", "DO NOT CARE", "FUGGEDDABOUDIT"}},
		{name: "nil regex value", value: nil, wantErr: true},
		{name: "pointer-to-string regex value", value: &myString, wantErr: true},
		{name: "integer regex value", value: 42, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := &Classify{
				SelectorTag:     "host",
				SelectorMapping: []map[string]string{{`pg\d{3}`: "database"}},
				MatchField:      "message",
				ResultTag:       "severity",
				MappedSelectorRegexes: map[string][]map[string]interface{}{
					"test-group": {{"ignore": tt.value}},
				},
			}
			if testing.Verbose() {
				cl.Log = testutil.Logger{}
			}
			if tt.wantErr {
				require.Error(t, cl.Init())
				return
			}
			acc := &testutil.Accumulator{}
			require.NoError(t, cl.Init())
			require.NoError(t, cl.Start(acc))
			cl.Stop()
		})
	}
}

// TestBadCategoryRegex verifies that invalid category regex content is rejected.
func TestBadCategoryRegex(t *testing.T) {
	tests := []struct {
		name    string
		entries []map[string]interface{}
	}{
		{
			name:    "duplicate category in same group",
			entries: []map[string]interface{}{{"ignore": "foobar"}, {"ignore": "barfoo"}},
		},
		{
			name:    "bad category regex",
			entries: []map[string]interface{}{{"ignore": "*foobar"}},
		},
		{
			name:    "empty category regex",
			entries: []map[string]interface{}{{"ignore": ""}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := &Classify{
				SelectorTag:     "host",
				SelectorMapping: []map[string]string{{`pg\d{3}`: "database"}},
				MatchField:      "message",
				ResultTag:       "severity",
				MappedSelectorRegexes: map[string][]map[string]interface{}{
					"test-group": tt.entries,
				},
			}
			if testing.Verbose() {
				cl.Log = testutil.Logger{}
			}
			require.Error(t, cl.Init())
		})
	}
}

// TestSelectorMapping exercises the full range of selector_mapping behaviours.
func TestSelectorMapping(t *testing.T) {
	msr := map[string][]map[string]interface{}{
		"database": {
			{"ignore": "IGNORE"}, {"okay": "OK"}, {"warning": "WARNING"},
			{"critical": "CRITICAL"}, {"unknown": ".*"},
		},
	}
	m := metric.New("datapoint",
		map[string]string{"host": "pg123"},
		map[string]interface{}{"message": "WARNING:  badness happened"},
		time.Now())

	tests := []struct {
		name            string
		selectorMapping []map[string]string
		defaultGroup    string
		wantCount       int
	}{
		{
			name:      "no selector_mapping and no default_regex_group",
			wantCount: 0,
		},
		{
			name:         "default_regex_group names nonexistent group",
			defaultGroup: "foobar",
			wantCount:    0,
		},
		{
			name:         "default_regex_group names existing group",
			defaultGroup: "database",
			wantCount:    1,
		},
		{
			name:            "selector matches entry",
			selectorMapping: []map[string]string{{`pg123`: "database"}},
			wantCount:       1,
		},
		{
			name:            "selector matches nothing, no default",
			selectorMapping: []map[string]string{{`abcde`: "database"}},
			wantCount:       0,
		},
		{
			name:            "selector matches nothing, valid default",
			selectorMapping: []map[string]string{{`abcde`: "database"}},
			defaultGroup:    "database",
			wantCount:       1,
		},
		{
			name:            "selector maps to empty string",
			selectorMapping: []map[string]string{{`pg123`: ""}},
			wantCount:       0,
		},
		{
			name:            "selector maps to *, no default",
			selectorMapping: []map[string]string{{`pg123`: "*"}},
			wantCount:       0,
		},
		{
			name:            "selector maps to *, valid default",
			selectorMapping: []map[string]string{{`pg123`: "*"}},
			defaultGroup:    "database",
			wantCount:       1,
		},
		{
			name:            "selector maps to unknown group",
			selectorMapping: []map[string]string{{`pg123`: "foobar"}},
			wantCount:       0,
		},
		{
			name:            "selector uses valid regex",
			selectorMapping: []map[string]string{{`pg\d{3}`: "database"}},
			wantCount:       1,
		},
		{
			name: "multiple ordered selector entries",
			selectorMapping: []map[string]string{
				{`fire\d{3}`: "firewall"},
				{`desk\d{3}`: "desktop"},
				{`pg\d{3}`: "database"},
			},
			wantCount: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := &Classify{
				SelectorTag:           "host",
				SelectorMapping:       tt.selectorMapping,
				DefaultRegexGroup:     tt.defaultGroup,
				MatchField:            "message",
				ResultTag:             "severity",
				MappedSelectorRegexes: msr,
			}
			if testing.Verbose() {
				cl.Log = testutil.Logger{}
			}
			acc := runClassifyTest(t, cl, []telegraf.Metric{m})
			require.Len(t, acc.GetTelegrafMetrics(), tt.wantCount)
		})
	}
}

// TestDefaultCategory verifies that default_category is applied when no regex
// matches, and that metrics are dropped when it is not configured.
func TestDefaultCategory(t *testing.T) {
	msr := map[string][]map[string]interface{}{
		"database": {
			{"ignore": "IGNORE"}, {"okay": "OKAY"},
			{"warning": "WARNING"}, {"critical": "CRITICAL"}, {"unknown": "UNKNOWN"},
		},
	}
	m := metric.New("datapoint",
		map[string]string{"host": "pg123"},
		map[string]interface{}{"message": "this message contains no category name"},
		time.Now())

	tests := []struct {
		name            string
		defaultCategory string
		wantCount       int
	}{
		{name: "no default_category", wantCount: 0},
		{name: "default_category set", defaultCategory: "unmatched", wantCount: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := &Classify{
				SelectorTag:           "host",
				SelectorMapping:       []map[string]string{{`pg\d{3}`: "database"}},
				MatchField:            "message",
				ResultTag:             "severity",
				DefaultCategory:       tt.defaultCategory,
				MappedSelectorRegexes: msr,
			}
			if testing.Verbose() {
				cl.Log = testutil.Logger{}
			}
			acc := runClassifyTest(t, cl, []telegraf.Metric{m})
			require.Len(t, acc.GetTelegrafMetrics(), tt.wantCount)
			if tt.wantCount > 0 {
				got := acc.GetTelegrafMetrics()[0]
				resultTag, ok := got.GetTag(cl.ResultTag)
				require.Truef(t, ok, "result tag %q not found", cl.ResultTag)
				require.Equal(t, tt.defaultCategory, resultTag)
			}
		})
	}
}

// TestAggregationSummary verifies basic summary aggregation: counters are
// emitted at the end of a period and the metric has the expected shape.
func TestAggregationSummary(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping aggregation test in short mode")
	}

	cl := &Classify{
		SelectorTag:     "host",
		SelectorMapping: []map[string]string{{`pg\d{3}`: "database"}},
		MatchField:      "message",
		DropCategories:  []string{"ignore", "unknown"},
		ResultTag:       "severity",
		MappedSelectorRegexes: map[string][]map[string]interface{}{
			"database": {
				{"ignore": "IGNORE"},
				{"okay": "OK"},
				{"warning": "WARNING"},
				{"critical": "CRITICAL"},
				{"unknown": ".*"},
			},
		},
		AggregationPeriod:       "5s",
		AggregationMeasurement:  "status",
		AggregationDroppedField: "dropped",
		AggregationTotalField:   "total",
		AggregationSummaryTag:   "summary",
		AggregationSummaryValue: "full",
		AggregationSummaryFields: []string{
			"ignore", "okay", "warning", "critical", "unknown", "dropped", "total",
		},
	}
	if testing.Verbose() {
		cl.Log = testutil.Logger{}
	}

	m := metric.New("datapoint",
		map[string]string{"host": "pg123"},
		map[string]interface{}{"message": "nothing to see here, move along"},
		time.Now())

	acc := runClassifyTest(t, cl, []telegraf.Metric{m}, 10*time.Second)

	allMetrics := acc.GetTelegrafMetrics()
	errMsg := metricsErrMsg(allMetrics)
	require.Equal(t, 1, len(allMetrics), errMsg)

	got := allMetrics[0]
	require.Equal(t, "status", got.Name(), errMsg)
	require.EqualValuesf(t, 1, len(got.TagList()), "tag count; %s", errMsg)
	require.EqualValuesf(t, 3, len(got.FieldList()), "field count; %s", errMsg)

	summaryTag, ok := got.GetTag("summary")
	require.Truef(t, ok, "summary tag missing; %s", errMsg)
	require.Equal(t, "full", summaryTag, errMsg)

	dropped, ok := got.GetField("dropped")
	require.Truef(t, ok, "dropped field missing; %s", errMsg)
	require.EqualValues(t, 1, dropped, errMsg)

	total, ok := got.GetField("total")
	require.Truef(t, ok, "total field missing; %s", errMsg)
	require.EqualValues(t, 1, total, errMsg)

	unknown, ok := got.GetField("unknown")
	require.Truef(t, ok, "unknown field missing; %s", errMsg)
	require.EqualValues(t, 1, unknown, errMsg)
}

// TestAggregationSummaryCycles verifies that counters reset between periods
// and that two successive periods produce independent metrics.
func TestAggregationSummaryCycles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping aggregation test in short mode")
	}

	cl := &Classify{
		SelectorTag:     "host",
		SelectorMapping: []map[string]string{{`pg\d{3}`: "database"}},
		MatchField:      "message",
		DropCategories:  []string{"ignore", "unknown"},
		ResultTag:       "severity",
		MappedSelectorRegexes: map[string][]map[string]interface{}{
			"database": {
				{"ignore": "IGNORE"},
				{"okay": "OK"},
				{"warning": "WARNING"},
				{"critical": "CRITICAL"},
				{"unknown": ".*"},
			},
		},
		AggregationPeriod:       "5s",
		AggregationMeasurement:  "status",
		AggregationDroppedField: "dropped",
		AggregationTotalField:   "total",
		AggregationSummaryTag:   "summary",
		AggregationSummaryValue: "full",
		AggregationSummaryFields: []string{
			"ignore", "okay", "warning", "critical", "unknown", "dropped", "total",
		},
	}
	if testing.Verbose() {
		cl.Log = testutil.Logger{}
	}

	m1 := metric.New("datapoint",
		map[string]string{"host": "pg123"},
		map[string]interface{}{"message": "nothing to see here, move along"},
		time.Now())
	m2 := metric.New("datapoint",
		map[string]string{"host": "pg123"},
		map[string]interface{}{"message": "CRITICAL:  second message from the same host"},
		time.Now())

	acc := &testutil.Accumulator{}
	require.NoError(t, cl.Init())
	require.NoError(t, cl.Start(acc))

	require.NoError(t, cl.Add(m1, acc))
	time.Sleep(7 * time.Second)
	require.NoError(t, cl.Add(m2, acc))
	cl.Stop()

	allMetrics := acc.GetTelegrafMetrics()
	errMsg := metricsErrMsg(allMetrics)
	require.Equal(t, 3, len(allMetrics), errMsg)

	// First metric: summary for first period (m1 dropped as unknown).
	got := allMetrics[0]
	require.Equal(t, "status", got.Name(), errMsg)
	require.EqualValuesf(t, 1, len(got.TagList()), "tag count; %s", errMsg)
	require.EqualValuesf(t, 3, len(got.FieldList()), "field count; %s", errMsg)
	summaryTag, ok := got.GetTag("summary")
	require.Truef(t, ok, "summary tag missing; %s", errMsg)
	require.Equal(t, "full", summaryTag, errMsg)
	dropped, ok := got.GetField("dropped")
	require.Truef(t, ok, "dropped missing; %s", errMsg)
	require.EqualValues(t, 1, dropped, errMsg)
	total, ok := got.GetField("total")
	require.Truef(t, ok, "total missing; %s", errMsg)
	require.EqualValues(t, 1, total, errMsg)

	// Second metric: m2 passed through as "critical".
	got = allMetrics[1]
	require.Equal(t, "datapoint", got.Name(), errMsg)
	require.EqualValuesf(t, 2, len(got.TagList()), "tag count; %s", errMsg)
	require.EqualValuesf(t, 1, len(got.FieldList()), "field count; %s", errMsg)
	hostTag, ok := got.GetTag("host")
	require.Truef(t, ok, "host tag missing; %s", errMsg)
	require.Equal(t, "pg123", hostTag, errMsg)
	resultTag, ok := got.GetTag("severity")
	require.Truef(t, ok, "severity tag missing; %s", errMsg)
	require.Equal(t, "critical", resultTag, errMsg)

	// Third metric: summary for second period (m2 classified as critical).
	got = allMetrics[2]
	require.Equal(t, "status", got.Name(), errMsg)
	require.EqualValuesf(t, 1, len(got.TagList()), "tag count; %s", errMsg)
	require.EqualValuesf(t, 2, len(got.FieldList()), "field count; %s", errMsg)
	summaryTag, ok = got.GetTag("summary")
	require.Truef(t, ok, "summary tag missing; %s", errMsg)
	require.Equal(t, "full", summaryTag, errMsg)
	critical, ok := got.GetField("critical")
	require.Truef(t, ok, "critical field missing; %s", errMsg)
	require.EqualValues(t, 1, critical, errMsg)
	total, ok = got.GetField("total")
	require.Truef(t, ok, "total missing; %s", errMsg)
	require.EqualValues(t, 1, total, errMsg)
}

// TestAggregationByGroup verifies per-regex-group aggregation counters.
func TestAggregationByGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping aggregation test in short mode")
	}

	cl := &Classify{
		SelectorTag: "host",
		SelectorMapping: []map[string]string{
			{`pg\d{3}`: "database"},
			{`fire\d{3}`: "firewall"},
		},
		MatchField:     "message",
		DropCategories: []string{"ignore", "unknown"},
		ResultTag:      "severity",
		MappedSelectorRegexes: map[string][]map[string]interface{}{
			"database": {
				{"ignore": "IGNORE"},
				{"okay": "OK"},
				{"warning": "WARNING"},
				{"critical": "CRITICAL"},
				{"unknown": ".*"},
			},
			"firewall": {
				{"ignore": "IGNORE"},
				{"okay": "OKAY"},
				{"warning": "LOGIN"},
				{"critical": "INTRUSION"},
				{"unknown": ".*"},
			},
		},
		AggregationPeriod:       "5s",
		AggregationMeasurement:  "status",
		AggregationDroppedField: "dropped",
		AggregationTotalField:   "total",
		AggregationGroupTag:     "by_machine_type",
		AggregationGroupFields: []string{
			"ignore", "okay", "warning", "critical", "unknown", "dropped", "total",
		},
	}
	if testing.Verbose() {
		cl.Log = testutil.Logger{}
	}

	now := time.Now()
	metrics := []telegraf.Metric{
		metric.New("datapoint",
			map[string]string{"host": "pg123"},
			map[string]interface{}{"message": "WARNING:  situation is crazy"},
			now),
		metric.New("datapoint",
			map[string]string{"host": "pg124"},
			map[string]interface{}{"message": "nothing to see here, move along"},
			now),
		metric.New("datapoint",
			map[string]string{"host": "fire567"},
			map[string]interface{}{"message": "INTRUSION:  assets at risk"},
			now),
	}

	acc := runClassifyTest(t, cl, metrics, 10*time.Second)

	allMetrics := acc.GetTelegrafMetrics()
	errMsg := metricsErrMsg(allMetrics)
	require.Equal(t, 4, len(allMetrics), errMsg)

	// First two are passthrough datapoints (order may vary).
	distinctSelector := make(map[string]int)
	for i := 0; i <= 1; i++ {
		got := allMetrics[i]
		require.Equal(t, "datapoint", got.Name(), errMsg)
		require.EqualValuesf(t, 2, len(got.TagList()), "tag count; %s", errMsg)

		selectorTag, ok := got.GetTag(cl.SelectorTag)
		require.Truef(t, ok, "host tag missing; %s", errMsg)
		resultTag, ok := got.GetTag(cl.ResultTag)
		require.Truef(t, ok, "severity tag missing; %s", errMsg)

		distinctSelector[selectorTag]++
		switch selectorTag {
		case "pg123":
			require.Equal(t, "warning", resultTag, errMsg)
		case "fire567":
			require.Equal(t, "critical", resultTag, errMsg)
		default:
			require.FailNowf(t, "unexpected selector tag value %q", selectorTag)
		}
	}
	require.Len(t, distinctSelector, 2, errMsg)

	// Last two are aggregation metrics by group (order may vary).
	distinctGroup := make(map[string]int)
	for i := 2; i <= 3; i++ {
		got := allMetrics[i]
		require.Equal(t, cl.AggregationMeasurement, got.Name(), errMsg)
		require.EqualValuesf(t, 1, len(got.TagList()), "tag count; %s", errMsg)

		groupTag, ok := got.GetTag(cl.AggregationGroupTag)
		require.Truef(t, ok, "group tag missing; %s", errMsg)

		distinctGroup[groupTag]++
		switch groupTag {
		case "database":
			require.EqualValuesf(t, 4, len(got.FieldList()), "database field count; %s", errMsg)
			dropped, ok := got.GetField(cl.AggregationDroppedField)
			require.Truef(t, ok, "dropped missing; %s", errMsg)
			require.EqualValues(t, 1, dropped, errMsg)
			total, ok := got.GetField(cl.AggregationTotalField)
			require.Truef(t, ok, "total missing; %s", errMsg)
			require.EqualValues(t, 2, total, errMsg)
		case "firewall":
			require.EqualValuesf(t, 2, len(got.FieldList()), "firewall field count; %s", errMsg)
			critical, ok := got.GetField("critical")
			require.Truef(t, ok, "critical missing; %s", errMsg)
			require.EqualValues(t, 1, critical, errMsg)
		default:
			require.FailNowf(t, "unexpected group tag value %q", groupTag)
		}
	}
	require.Len(t, distinctGroup, 2, errMsg)
}

// TestAggregationBySelector verifies per-selector aggregation counters.
func TestAggregationBySelector(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping aggregation test in short mode")
	}

	cl := &Classify{
		SelectorTag:     "host",
		SelectorMapping: []map[string]string{{`pg\d{3}`: "database"}},
		MatchField:      "message",
		DropCategories:  []string{"ignore", "unknown"},
		ResultTag:       "severity",
		MappedSelectorRegexes: map[string][]map[string]interface{}{
			"database": {
				{"ignore": "IGNORE"},
				{"okay": "OK"},
				{"warning": "WARNING"},
				{"critical": "CRITICAL"},
				{"unknown": ".*"},
			},
		},
		AggregationPeriod:       "5s",
		AggregationMeasurement:  "status",
		AggregationDroppedField: "dropped",
		AggregationTotalField:   "total",
		AggregationSelectorTag:  "by_host",
		AggregationSelectorFields: []string{
			"ignore", "okay", "warning", "critical", "unknown", "dropped", "total",
		},
	}
	if testing.Verbose() {
		cl.Log = testutil.Logger{}
	}

	now := time.Now()
	metrics := []telegraf.Metric{
		metric.New("datapoint",
			map[string]string{"host": "pg123"},
			map[string]interface{}{"message": "WARNING:  situation is crazy"},
			now),
		metric.New("datapoint",
			map[string]string{"host": "pg123"},
			map[string]interface{}{"message": "nothing to see here, move along"},
			now),
		metric.New("datapoint",
			map[string]string{"host": "pg124"},
			map[string]interface{}{"message": "nothing to see here, move along"},
			now),
	}

	acc := runClassifyTest(t, cl, metrics, 10*time.Second)

	allMetrics := acc.GetTelegrafMetrics()
	errMsg := metricsErrMsg(allMetrics)
	require.Equal(t, 3, len(allMetrics), errMsg)

	// First is the passthrough datapoint.
	got := allMetrics[0]
	require.Equal(t, "datapoint", got.Name(), errMsg)
	selectorTag, ok := got.GetTag(cl.SelectorTag)
	require.Truef(t, ok, "host tag missing; %s", errMsg)
	require.Equal(t, "pg123", selectorTag, errMsg)
	resultTag, ok := got.GetTag(cl.ResultTag)
	require.Truef(t, ok, "severity tag missing; %s", errMsg)
	require.Equal(t, "warning", resultTag, errMsg)

	// Remaining two are per-selector aggregation metrics (order may vary).
	distinctSelector := make(map[string]int)
	for i := 1; i <= 2; i++ {
		got = allMetrics[i]
		require.Equal(t, cl.AggregationMeasurement, got.Name(), errMsg)
		require.EqualValuesf(t, 1, len(got.TagList()), "tag count; %s", errMsg)

		sel, ok := got.GetTag(cl.AggregationSelectorTag)
		require.Truef(t, ok, "selector tag missing; %s", errMsg)
		distinctSelector[sel]++

		switch sel {
		case "pg123":
			require.EqualValuesf(t, 4, len(got.FieldList()), "pg123 field count; %s", errMsg)
			dropped, ok := got.GetField(cl.AggregationDroppedField)
			require.Truef(t, ok, "dropped missing; %s", errMsg)
			require.EqualValues(t, 1, dropped, errMsg)
			total, ok := got.GetField(cl.AggregationTotalField)
			require.Truef(t, ok, "total missing; %s", errMsg)
			require.EqualValues(t, 2, total, errMsg)
		case "pg124":
			require.EqualValuesf(t, 3, len(got.FieldList()), "pg124 field count; %s", errMsg)
			dropped, ok := got.GetField(cl.AggregationDroppedField)
			require.Truef(t, ok, "dropped missing; %s", errMsg)
			require.EqualValues(t, 1, dropped, errMsg)
		default:
			require.FailNowf(t, "unexpected selector tag value %q", sel)
		}
	}
	require.Len(t, distinctSelector, 2, errMsg)
}

// TestAggregationDroppedAndTotalFields verifies that dropped and total counters
// track the right counts across a mix of passed and dropped metrics.
func TestAggregationDroppedAndTotalFields(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping aggregation test in short mode")
	}

	cl := &Classify{
		SelectorTag:     "host",
		SelectorMapping: []map[string]string{{`pg\d{3}`: "database"}},
		MatchField:      "message",
		DropCategories:  []string{"ignore", "unknown"},
		ResultTag:       "severity",
		MappedSelectorRegexes: map[string][]map[string]interface{}{
			"database": {
				{"ignore": "IGNORE"},
				{"okay": "OK"},
				{"warning": "WARNING"},
				{"critical": "CRITICAL"},
				{"unknown": ".*"},
			},
		},
		AggregationPeriod:       "5s",
		AggregationMeasurement:  "status",
		AggregationDroppedField: "dropped",
		AggregationTotalField:   "total",
		AggregationSummaryTag:   "summary",
		AggregationSummaryValue: "full",
		AggregationSummaryFields: []string{
			"ignore", "okay", "warning", "critical", "unknown", "dropped", "total",
		},
	}
	if testing.Verbose() {
		cl.Log = testutil.Logger{}
	}

	now := time.Now()
	metrics := []telegraf.Metric{
		metric.New("datapoint",
			map[string]string{"host": "pg123"},
			map[string]interface{}{"message": "WARNING:  situation is crazy"},
			now),
		metric.New("datapoint",
			map[string]string{"host": "pg123"},
			map[string]interface{}{"message": "nothing to see here, move along"},
			now),
		metric.New("datapoint",
			map[string]string{"host": "pg123"},
			map[string]interface{}{"message": "nothing to see here, move along"},
			now),
	}

	acc := runClassifyTest(t, cl, metrics, 10*time.Second)

	allMetrics := acc.GetTelegrafMetrics()
	errMsg := metricsErrMsg(allMetrics)
	require.Equal(t, 2, len(allMetrics), errMsg)

	// First: the passed-through warning metric.
	got := allMetrics[0]
	require.Equal(t, "datapoint", got.Name(), errMsg)
	require.EqualValuesf(t, 2, len(got.TagList()), "tag count; %s", errMsg)
	require.EqualValuesf(t, 1, len(got.FieldList()), "field count; %s", errMsg)
	resultTag, ok := got.GetTag(cl.ResultTag)
	require.Truef(t, ok, "severity tag missing; %s", errMsg)
	require.Equal(t, "warning", resultTag, errMsg)

	// Second: the summary aggregation metric.
	got = allMetrics[1]
	require.Equal(t, cl.AggregationMeasurement, got.Name(), errMsg)
	require.EqualValuesf(t, 1, len(got.TagList()), "tag count; %s", errMsg)
	require.EqualValuesf(t, 4, len(got.FieldList()), "field count; %s", errMsg)

	summaryTag, ok := got.GetTag(cl.AggregationSummaryTag)
	require.Truef(t, ok, "summary tag missing; %s", errMsg)
	require.Equal(t, cl.AggregationSummaryValue, summaryTag, errMsg)

	dropped, ok := got.GetField(cl.AggregationDroppedField)
	require.Truef(t, ok, "dropped missing; %s", errMsg)
	require.EqualValues(t, 2, dropped, errMsg)

	total, ok := got.GetField(cl.AggregationTotalField)
	require.Truef(t, ok, "total missing; %s", errMsg)
	require.EqualValues(t, 3, total, errMsg)

	unknown, ok := got.GetField("unknown")
	require.Truef(t, ok, "unknown missing; %s", errMsg)
	require.EqualValues(t, 2, unknown, errMsg)

	warning, ok := got.GetField("warning")
	require.Truef(t, ok, "warning missing; %s", errMsg)
	require.EqualValues(t, 1, warning, errMsg)
}

// metricsErrMsg formats all metrics into a readable string for assertion messages.
func metricsErrMsg(metrics []telegraf.Metric) string {
	msg := "output metrics:\n"
	for _, m := range metrics {
		msg += fmt.Sprintf("  %v\n", m)
	}
	return msg
}
