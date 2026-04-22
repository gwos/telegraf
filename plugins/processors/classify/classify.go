//go:generate ../../../tools/readme_config_includer/generator
package classify

import (
	_ "embed"
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/processors"
)

// DO NOT REMOVE THE NEXT TWO LINES! This is required to embed the sampleConfig data.
//go:embed sample.conf
var sampleConfig string

type Classify struct {
	// Selector: which tag/field value determines the regex group to use.
	// Mutually exclusive; omit both to use DefaultRegexGroup unconditionally.
	SelectorTag   string `toml:"selector_tag"`
	SelectorField string `toml:"selector_field"`

	// Ordered regex-to-group mapping applied to the selector value.
	// Each element must contain exactly one key.
	SelectorMapping []map[string]string `toml:"selector_mapping"`

	// Regex group to fall back to when no selector mapping matches.
	DefaultRegexGroup string `toml:"default_regex_group"`

	// Match: which tag/field value is tested against category regexes.
	// Exactly one must be defined.
	MatchTag   string `toml:"match_tag"`
	MatchField string `toml:"match_field"`

	// DefaultCategory is used when no regex matches. Metrics are dropped if empty.
	DefaultCategory string `toml:"default_category"`

	// DropCategories lists categories whose matched metrics are dropped.
	// Accepts a single string or an array of strings in TOML.
	DropCategories interface{} `toml:"drop_categories"`

	// Result: where to write the classification outcome.
	// Exactly one must be defined.
	ResultTag   string `toml:"result_tag"`
	ResultField string `toml:"result_field"`

	// MappedSelectorRegexes maps each group name to an ordered list of
	// category→regex definitions. Regex values may be a single string,
	// a multi-line string (one regex per line), or an array of strings.
	MappedSelectorRegexes map[string][]map[string]interface{} `toml:"mapped_selector_regexes"`

	// Aggregation options. All are optional; aggregation is disabled unless
	// AggregationPeriod and AggregationMeasurement are both set together with
	// at least one of the summary/group/selector field sets.
	AggregationPeriod         string   `toml:"aggregation_period"`
	AggregationMeasurement    string   `toml:"aggregation_measurement"`
	AggregationDroppedField   string   `toml:"aggregation_dropped_field"`
	AggregationTotalField     string   `toml:"aggregation_total_field"`
	AggregationSummaryTag     string   `toml:"aggregation_summary_tag"`
	AggregationSummaryValue   string   `toml:"aggregation_summary_value"`
	AggregationSummaryFields  []string `toml:"aggregation_summary_fields"`
	AggregationGroupTag       string   `toml:"aggregation_group_tag"`
	AggregationGroupFields    []string `toml:"aggregation_group_fields"`
	AggregationSelectorTag    string   `toml:"aggregation_selector_tag"`
	AggregationSelectorFields []string `toml:"aggregation_selector_fields"`
	AggregationIncludesZeroes bool     `toml:"aggregation_includes_zeroes"`

	Log telegraf.Logger `toml:"-"`

	// Internal state derived from config fields during Init.
	acc                   telegraf.Accumulator
	selectorMap           []map[*regexp.Regexp]string
	dropThisCategory      map[string]bool
	groupCategoriesSeen   map[string]map[string]bool
	mappedRegexes         map[string][]map[string][]*regexp.Regexp
	allRegexCategories    map[string]bool
	aggregationTimePeriod time.Duration
	doAggregation         bool
	doSummaryAggregation  bool
	doGroupAggregation    bool
	doSelectorAggregation bool

	// sharedData is accessed by both Add() and the aggregation goroutine;
	// all access must hold aggregationMutex.
	sharedData struct {
		aggregationMutex      sync.Mutex
		aggregationSummary    map[string]int
		aggregationByGroup    map[string]map[string]int
		aggregationBySelector map[string]map[string]int
	}

	syncWaitGroup sync.WaitGroup
	stopRequested chan bool
}

func (*Classify) SampleConfig() string {
	return sampleConfig
}

// Init validates config and compiles all regular expressions.
// It may be called multiple times on the same instance (e.g. in tests),
// clearing all previously derived state each time.
func (cl *Classify) Init() error {
	cl.selectorMap = nil
	cl.dropThisCategory = nil
	cl.groupCategoriesSeen = nil
	cl.mappedRegexes = nil
	cl.allRegexCategories = nil
	cl.aggregationTimePeriod = 0
	cl.doAggregation = false
	cl.doSummaryAggregation = false
	cl.doGroupAggregation = false
	cl.doSelectorAggregation = false
	cl.sharedData.aggregationSummary = nil
	cl.sharedData.aggregationByGroup = nil
	cl.sharedData.aggregationBySelector = nil

	if err := cl.initClassification(); err != nil {
		return err
	}
	return cl.initAggregation()
}

// Start stores the accumulator and launches the aggregation goroutine if needed.
func (cl *Classify) Start(acc telegraf.Accumulator) error {
	cl.acc = acc
	if cl.doAggregation {
		cl.stopRequested = make(chan bool)
		cl.syncWaitGroup.Add(1)
		go cl.runAggregation()
	}
	return nil
}

// Add classifies one metric and either passes it downstream or drops it.
func (cl *Classify) Add(metric telegraf.Metric, _ telegraf.Accumulator) error {
	dropPoint := false

	selectorItemValue := ""
	regexGroup := ""
	haveRegexGroup := false
	switch {
	case cl.SelectorTag != "":
		if value, ok := metric.GetTag(cl.SelectorTag); ok {
			selectorItemValue = value
		} else {
			if cl.Log != nil {
				cl.Log.Infof("Dropping point (selector tag %q is missing)", cl.SelectorTag)
			}
			dropPoint = true
		}
	case cl.SelectorField != "":
		if value, ok := metric.GetField(cl.SelectorField); ok {
			if v, ok := value.(string); ok {
				selectorItemValue = v
			} else {
				if cl.Log != nil {
					cl.Log.Infof("Dropping point (selector field %q is not a string)", cl.SelectorField)
				}
				dropPoint = true
			}
		} else {
			if cl.Log != nil {
				cl.Log.Infof("Dropping point (selector field %q is missing)", cl.SelectorField)
			}
			dropPoint = true
		}
	default:
		// Neither selector_tag nor selector_field; use default_regex_group directly.
		regexGroup = cl.DefaultRegexGroup
		haveRegexGroup = true
	}

	if !dropPoint && !haveRegexGroup {
		var matchedRegex *regexp.Regexp
		for _, mapping := range cl.selectorMap {
			for regex, group := range mapping {
				if regex.MatchString(selectorItemValue) {
					matchedRegex = regex
					if group == "*" {
						regexGroup = selectorItemValue
					} else {
						regexGroup = group
					}
					break
				}
			}
		}
		if matchedRegex == nil {
			if cl.Log != nil {
				cl.Log.Infof("Selector item value %q does not match anything in selector_mapping", selectorItemValue)
			}
			regexGroup = cl.DefaultRegexGroup
		}
	}

	if !dropPoint {
		if regexGroup == "" {
			if cl.Log != nil {
				cl.Log.Infof("Dropping point (selector item value %q maps to an empty regex group)", selectorItemValue)
			}
			dropPoint = true
		} else if _, ok := cl.mappedRegexes[regexGroup]; !ok {
			if cl.DefaultRegexGroup == "" {
				if cl.Log != nil {
					cl.Log.Infof("Dropping point (selector %q maps to %q, which is not a known regex group)",
						selectorItemValue, regexGroup)
				}
				dropPoint = true
			} else if _, ok := cl.mappedRegexes[cl.DefaultRegexGroup]; !ok {
				if cl.Log != nil {
					cl.Log.Infof("Dropping point (selector %q maps to %q; default_regex_group %q is also unknown)",
						selectorItemValue, regexGroup, cl.DefaultRegexGroup)
				}
				dropPoint = true
			} else {
				regexGroup = cl.DefaultRegexGroup
			}
		}
	}

	matchItemValue := ""
	if !dropPoint {
		switch {
		case cl.MatchTag != "":
			if value, ok := metric.GetTag(cl.MatchTag); ok {
				matchItemValue = value
			} else {
				if cl.Log != nil {
					cl.Log.Infof("Dropping point (match tag %q is missing)", cl.MatchTag)
				}
				dropPoint = true
			}
		case cl.MatchField != "":
			if value, ok := metric.GetField(cl.MatchField); ok {
				if v, ok := value.(string); ok {
					matchItemValue = v
				} else {
					if cl.Log != nil {
						cl.Log.Infof("Dropping point (match field %q is not a string)", cl.MatchField)
					}
					dropPoint = true
				}
			} else {
				if cl.Log != nil {
					cl.Log.Infof("Dropping point (match field %q is missing)", cl.MatchField)
				}
				dropPoint = true
			}
		default:
			// Guarded by Init() — should never happen in production.
			if cl.Log != nil {
				cl.Log.Error("Internal error: neither match_tag nor match_field is set")
			}
			return fmt.Errorf("internal error: neither match_tag nor match_field is set")
		}
	}

	matchedCategory := ""
	if !dropPoint {
		if cl.Log != nil {
			cl.Log.Debug("Attempting category regex matches")
		}
		categoryList, ok := cl.mappedRegexes[regexGroup]
		if !ok {
			// Guarded by earlier checks — should never happen.
			if cl.Log != nil {
				cl.Log.Errorf("Internal error: regex group %q not found after validation", regexGroup)
			}
			return fmt.Errorf("internal error: regex group %q not found after validation", regexGroup)
		}
		if cl.Log != nil {
			cl.Log.Debugf("Using regex group %q (%d categories)", regexGroup, len(categoryList))
		}
	matchLoop:
		for _, categoryDefinition := range categoryList {
			for category, categoryRegexes := range categoryDefinition {
				for _, regex := range categoryRegexes {
					if regex.MatchString(matchItemValue) {
						matchedCategory = category
						break matchLoop
					}
				}
			}
		}
	}

	if !dropPoint {
		if matchedCategory == "" {
			matchedCategory = cl.DefaultCategory
			if cl.Log != nil {
				cl.Log.Debugf("No regex match; using default_category %q", cl.DefaultCategory)
			}
		} else {
			if cl.Log != nil {
				cl.Log.Debugf("Matched category %q", matchedCategory)
			}
		}
		if matchedCategory == "" {
			if cl.Log != nil {
				cl.Log.Debug("Dropping point (no match and default_category is not set)")
			}
			dropPoint = true
		} else if cl.dropThisCategory[matchedCategory] {
			if cl.Log != nil {
				cl.Log.Debugf("Dropping point (category %q is in drop_categories)", matchedCategory)
			}
			dropPoint = true
		}
	}

	if !dropPoint {
		if cl.ResultTag != "" {
			metric.AddTag(cl.ResultTag, matchedCategory)
		} else if cl.ResultField != "" {
			metric.AddField(cl.ResultField, matchedCategory)
		}
		cl.acc.AddMetric(metric)
	} else {
		metric.Drop()
	}

	if cl.doAggregation {
		cl.sharedData.aggregationMutex.Lock()
		defer cl.sharedData.aggregationMutex.Unlock()

		if cl.doSummaryAggregation {
			if matchedCategory != "" {
				cl.sharedData.aggregationSummary[matchedCategory]++
			}
			if dropPoint && cl.AggregationDroppedField != "" {
				cl.sharedData.aggregationSummary[cl.AggregationDroppedField]++
			}
			if cl.AggregationTotalField != "" {
				cl.sharedData.aggregationSummary[cl.AggregationTotalField]++
			}
		}

		if cl.doGroupAggregation && regexGroup != "" {
			if matchedCategory != "" {
				if cl.sharedData.aggregationByGroup[regexGroup] == nil {
					cl.sharedData.aggregationByGroup[regexGroup] = make(map[string]int)
				}
				cl.sharedData.aggregationByGroup[regexGroup][matchedCategory]++
			}
			if dropPoint && cl.AggregationDroppedField != "" {
				if cl.sharedData.aggregationByGroup[regexGroup] == nil {
					cl.sharedData.aggregationByGroup[regexGroup] = make(map[string]int)
				}
				cl.sharedData.aggregationByGroup[regexGroup][cl.AggregationDroppedField]++
			}
			if cl.AggregationTotalField != "" {
				if cl.sharedData.aggregationByGroup[regexGroup] == nil {
					cl.sharedData.aggregationByGroup[regexGroup] = make(map[string]int)
				}
				cl.sharedData.aggregationByGroup[regexGroup][cl.AggregationTotalField]++
			}
		}

		if cl.doSelectorAggregation && selectorItemValue != "" {
			if matchedCategory != "" {
				if cl.sharedData.aggregationBySelector[selectorItemValue] == nil {
					cl.sharedData.aggregationBySelector[selectorItemValue] = make(map[string]int)
				}
				cl.sharedData.aggregationBySelector[selectorItemValue][matchedCategory]++
			}
			if dropPoint && cl.AggregationDroppedField != "" {
				if cl.sharedData.aggregationBySelector[selectorItemValue] == nil {
					cl.sharedData.aggregationBySelector[selectorItemValue] = make(map[string]int)
				}
				cl.sharedData.aggregationBySelector[selectorItemValue][cl.AggregationDroppedField]++
			}
			if cl.AggregationTotalField != "" {
				if cl.sharedData.aggregationBySelector[selectorItemValue] == nil {
					cl.sharedData.aggregationBySelector[selectorItemValue] = make(map[string]int)
				}
				cl.sharedData.aggregationBySelector[selectorItemValue][cl.AggregationTotalField]++
			}
		}
	}

	return nil
}

// Stop signals the aggregation goroutine to finish and waits for it to exit.
func (cl *Classify) Stop() {
	if cl.doAggregation {
		cl.stopRequested <- true
		cl.syncWaitGroup.Wait()
	}
}

// saveRegexes compiles and stores all regexes for a {group, category} pair.
func (cl *Classify) saveRegexes(group, category string, allRegexes []string) error {
	if cl.groupCategoriesSeen == nil {
		cl.groupCategoriesSeen = make(map[string]map[string]bool)
	}
	if cl.groupCategoriesSeen[group] == nil {
		cl.groupCategoriesSeen[group] = make(map[string]bool)
	}
	if cl.groupCategoriesSeen[group][category] {
		return fmt.Errorf("duplicate category %q in mapped_selector_regexes group %q", category, group)
	}
	cl.groupCategoriesSeen[group][category] = true

	var regexPtrs []*regexp.Regexp
	for _, regex := range allRegexes {
		if regex == "" {
			return fmt.Errorf("empty regex in mapped_selector_regexes group %q category %q", group, category)
		}
		r, err := regexp.Compile(regex)
		if err != nil {
			return fmt.Errorf("invalid regex %q in group %q category %q: %w", regex, group, category, err)
		}
		regexPtrs = append(regexPtrs, r)
	}
	if len(regexPtrs) > 0 {
		if cl.mappedRegexes == nil {
			cl.mappedRegexes = make(map[string][]map[string][]*regexp.Regexp)
		}
		cl.mappedRegexes[group] = append(cl.mappedRegexes[group], map[string][]*regexp.Regexp{category: regexPtrs})
	}
	return nil
}

func (cl *Classify) initClassification() error {
	if cl.SelectorTag != "" && cl.SelectorField != "" {
		return fmt.Errorf("selector_tag and selector_field cannot both be defined")
	}
	if cl.MatchTag == "" && cl.MatchField == "" {
		return fmt.Errorf("either match_tag or match_field must be defined")
	}
	if cl.MatchTag != "" && cl.MatchField != "" {
		return fmt.Errorf("match_tag and match_field cannot both be defined")
	}
	if cl.ResultTag == "" && cl.ResultField == "" {
		return fmt.Errorf("either result_tag or result_field must be defined")
	}
	if cl.ResultTag != "" && cl.ResultField != "" {
		return fmt.Errorf("result_tag and result_field cannot both be defined")
	}

	seenSelectorRegex := make(map[string]bool)
	cl.selectorMap = make([]map[*regexp.Regexp]string, 0)
	for _, mapping := range cl.SelectorMapping {
		if len(mapping) > 1 {
			return fmt.Errorf("selector_mapping element contains more than one key")
		}
		for regex, group := range mapping {
			if regex == "" {
				return fmt.Errorf("empty regex in selector_mapping for group %q", group)
			}
			if seenSelectorRegex[regex] {
				return fmt.Errorf("duplicate selector_mapping regex %q", regex)
			}
			seenSelectorRegex[regex] = true
			r, err := regexp.Compile(regex)
			if err != nil {
				return fmt.Errorf("invalid selector_mapping regex %q for group %q: %w", regex, group, err)
			}
			cl.selectorMap = append(cl.selectorMap, map[*regexp.Regexp]string{r: group})
		}
	}

	for group, categoryHashes := range cl.MappedSelectorRegexes {
		for _, categoryRegexes := range categoryHashes {
			for category, regexes := range categoryRegexes {
				switch v := regexes.(type) {
				case string:
					if strings.ContainsRune(v, '\n') {
						var lines []string
						for _, line := range strings.Split(v, "\n") {
							if s := strings.TrimSpace(line); s != "" {
								lines = append(lines, s)
							}
						}
						if err := cl.saveRegexes(group, category, lines); err != nil {
							return err
						}
					} else {
						if err := cl.saveRegexes(group, category, []string{v}); err != nil {
							return err
						}
					}
				case []string:
					if err := cl.saveRegexes(group, category, v); err != nil {
						return err
					}
				case []interface{}:
					strs := make([]string, 0, len(v))
					for _, elem := range v {
						s, ok := elem.(string)
						if !ok {
							return fmt.Errorf("non-string element in mapped_selector_regexes group %q category %q: %v", group, category, elem)
						}
						strs = append(strs, s)
					}
					if err := cl.saveRegexes(group, category, strs); err != nil {
						return err
					}
				default:
					return fmt.Errorf("invalid regex value type %T in mapped_selector_regexes group %q category %q", regexes, group, category)
				}
			}
		}
	}

	if cl.mappedRegexes == nil {
		return fmt.Errorf("mapped_selector_regexes has no groups with category regexes defined")
	}

	cl.allRegexCategories = make(map[string]bool)
	for _, categoryList := range cl.mappedRegexes {
		for _, categoryDef := range categoryList {
			for category := range categoryDef {
				cl.allRegexCategories[category] = true
			}
		}
	}

	cl.dropThisCategory = make(map[string]bool)
	if cl.DropCategories != nil {
		if s, ok := cl.DropCategories.(string); ok {
			cl.DropCategories = []string{s}
		} else if _, ok := cl.DropCategories.([]string); !ok {
			return fmt.Errorf("drop_categories must be a string or array of strings")
		}
		for _, category := range cl.DropCategories.([]string) {
			if category == "" {
				return fmt.Errorf("drop_categories contains an empty string")
			}
			if !cl.allRegexCategories[category] && category != cl.DefaultCategory {
				return fmt.Errorf("%q in drop_categories is not a known regex category or default_category", category)
			}
			cl.dropThisCategory[category] = true
		}
	}

	return nil
}

func (cl *Classify) initAggregation() error {
	if cl.AggregationPeriod != "" {
		d, err := time.ParseDuration(cl.AggregationPeriod)
		if err != nil {
			return fmt.Errorf("invalid aggregation_period: %w", err)
		}
		if d < time.Second {
			return fmt.Errorf("aggregation_period must be at least one second")
		}
		cl.aggregationTimePeriod = d
	}

	allLegal := make(map[string]bool)
	for k, v := range cl.allRegexCategories {
		allLegal[k] = v
	}

	if cl.AggregationDroppedField != "" {
		if cl.allRegexCategories[cl.AggregationDroppedField] {
			return fmt.Errorf("aggregation_dropped_field %q conflicts with a regex category name", cl.AggregationDroppedField)
		}
		if cl.AggregationDroppedField == cl.DefaultCategory {
			return fmt.Errorf("aggregation_dropped_field %q conflicts with default_category", cl.AggregationDroppedField)
		}
		allLegal[cl.AggregationDroppedField] = true
	}

	if cl.AggregationTotalField != "" {
		if cl.allRegexCategories[cl.AggregationTotalField] {
			return fmt.Errorf("aggregation_total_field %q conflicts with a regex category name", cl.AggregationTotalField)
		}
		if cl.AggregationTotalField == cl.AggregationDroppedField {
			return fmt.Errorf("aggregation_total_field %q conflicts with aggregation_dropped_field", cl.AggregationTotalField)
		}
		if cl.AggregationTotalField == cl.DefaultCategory {
			return fmt.Errorf("aggregation_total_field %q conflicts with default_category", cl.AggregationTotalField)
		}
		allLegal[cl.AggregationTotalField] = true
	}

	if (cl.AggregationSummaryTag == "") != (cl.AggregationSummaryValue == "") {
		return fmt.Errorf("aggregation_summary_tag and aggregation_summary_value must both be set or both be empty")
	}

	for _, field := range cl.AggregationSummaryFields {
		if field == "" {
			return fmt.Errorf("aggregation_summary_fields contains an empty string")
		}
		if !allLegal[field] {
			return fmt.Errorf("aggregation_summary_fields: %q is not a known category, aggregation_dropped_field, or aggregation_total_field", field)
		}
	}
	for _, field := range cl.AggregationGroupFields {
		if field == "" {
			return fmt.Errorf("aggregation_group_fields contains an empty string")
		}
		if !allLegal[field] {
			return fmt.Errorf("aggregation_group_fields: %q is not a known category, aggregation_dropped_field, or aggregation_total_field", field)
		}
	}
	for _, field := range cl.AggregationSelectorFields {
		if field == "" {
			return fmt.Errorf("aggregation_selector_fields contains an empty string")
		}
		if !allLegal[field] {
			return fmt.Errorf("aggregation_selector_fields: %q is not a known category, aggregation_dropped_field, or aggregation_total_field", field)
		}
	}

	if cl.aggregationTimePeriod != 0 && cl.AggregationMeasurement != "" {
		cl.doSummaryAggregation = cl.AggregationSummaryTag != "" &&
			cl.AggregationSummaryValue != "" &&
			len(cl.AggregationSummaryFields) != 0
		cl.doGroupAggregation = cl.AggregationGroupTag != "" &&
			len(cl.AggregationGroupFields) != 0
		cl.doSelectorAggregation = cl.AggregationSelectorTag != "" &&
			len(cl.AggregationSelectorFields) != 0
		cl.doAggregation = cl.doSummaryAggregation || cl.doGroupAggregation || cl.doSelectorAggregation
	}

	if cl.doAggregation {
		if cl.doSummaryAggregation {
			cl.sharedData.aggregationSummary = make(map[string]int)
			for _, category := range cl.AggregationSummaryFields {
				cl.sharedData.aggregationSummary[category] = 0
			}
		}
		if cl.doGroupAggregation {
			cl.sharedData.aggregationByGroup = make(map[string]map[string]int)
		}
		if cl.doSelectorAggregation {
			cl.sharedData.aggregationBySelector = make(map[string]map[string]int)
		}
	}

	return nil
}

// runAggregation is the goroutine that periodically emits aggregation metrics.
func (cl *Classify) runAggregation() {
	defer func() {
		if p := recover(); p != nil {
			if cl.Log != nil {
				cl.Log.Errorf("Panic in aggregation goroutine: %v\n%s", p, debug.Stack())
			}
		}
	}()
	defer cl.syncWaitGroup.Done()

	// Phase the initial tick to align with natural period boundaries
	// (e.g. 00:05:00, 00:10:00 for a 5-minute period).
	now := time.Now()
	timer := time.NewTimer(now.Add(cl.aggregationTimePeriod).Truncate(cl.aggregationTimePeriod).Sub(now))
	// Placeholder ticker with a huge duration; replaced after the first timer fires.
	ticker := time.NewTicker(1_000_000 * time.Hour)
	var t time.Time
	for quit := false; !quit; {
		select {
		case t = <-timer.C:
			ticker.Stop()
			ticker = time.NewTicker(cl.aggregationTimePeriod)
		case t = <-ticker.C:
		case quit = <-cl.stopRequested:
			t = time.Now()
		}
		cl.outputAggregationData(t)
	}
	timer.Stop()
	ticker.Stop()
}

func (cl *Classify) generateMetric(tagName, tagValue string, counters map[string]int, ts time.Time) {
	fields := make(map[string]interface{})
	haveNonzero := false
	for category, count := range counters {
		if count > 0 {
			haveNonzero = true
			fields[category] = count
		} else if cl.AggregationIncludesZeroes {
			fields[category] = count
		}
	}
	if haveNonzero {
		cl.acc.AddCounter(cl.AggregationMeasurement, fields,
			map[string]string{tagName: tagValue}, ts)
	}
}

func (cl *Classify) outputAggregationData(ts time.Time) {
	cl.sharedData.aggregationMutex.Lock()
	defer cl.sharedData.aggregationMutex.Unlock()

	if cl.doSummaryAggregation {
		cl.generateMetric(cl.AggregationSummaryTag, cl.AggregationSummaryValue,
			cl.sharedData.aggregationSummary, ts)
		for category := range cl.sharedData.aggregationSummary {
			cl.sharedData.aggregationSummary[category] = 0
		}
	}
	if cl.doGroupAggregation {
		for group, counts := range cl.sharedData.aggregationByGroup {
			cl.generateMetric(cl.AggregationGroupTag, group, counts, ts)
		}
		cl.sharedData.aggregationByGroup = make(map[string]map[string]int)
	}
	if cl.doSelectorAggregation {
		for selector, counts := range cl.sharedData.aggregationBySelector {
			cl.generateMetric(cl.AggregationSelectorTag, selector, counts, ts)
		}
		cl.sharedData.aggregationBySelector = make(map[string]map[string]int)
	}
}

func init() {
	processors.AddStreaming("classify", func() telegraf.StreamingProcessor {
		return &Classify{}
	})
}
