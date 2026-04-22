# Classify Processor Plugin

The `classify` plugin classifies metrics by matching a designated tag or field
value against groups of regular expressions. Each input metric either passes
through with a new result tag/field set to the matched category name, or is
dropped. The original data is never modified.

The plugin optionally supports a selector mapping step: a tag or field value
is mapped to the name of a regex group, allowing distinct sets of regexes to
be applied to different classes of input without repeating configuration.

In addition to classification, the plugin can accumulate per-period statistics
and emit them as separate metrics, acting as a lightweight aggregator.

Use `processors.template` for exact-match lookups and structural transforms,
and `processors.classify` for the ordered-regex / selector-dispatch use case.
They complement rather than substitute.

## Processing Model

```text
input metric
    │
    ├─ resolve regex group (selector → group mapping, or default_regex_group)
    │
    ├─ match against category regexes in that group (first match wins)
    │
    ├─ apply result tag/field to metric
    │
    └─ pass downstream  ─OR─  drop (if category in drop_categories or no match)
```

The selector resolves to one of several regex groups; only the selected group's
category regexes are tested against the match item:

```text
                    ┌─────────────────────────────────────┐
        selector ──►│   selector to regex-group mapping   │
                    └─────────────────────────────────────┘
                                        │
                                  regex group name
                                        │
                                        ▼
         ┌─ regex group 1 ────────────────────────────────┐
         │  ┌──────────┐  ┌──────────┐  ┌──────────┐     │
         │  │categoryA │  │categoryB │  │categoryC │     │
         │  │ regexes  │  │ regexes  │  │ regexes  │     │
         │  └──────────┘  └──────────┘  └──────────┘     │
         └────────────────────────────────────────────────┘

match ──►┌─ regex group 2 (selected) ─────────────────────┐──► result
item     │  ┌──────────┐  ┌──────────┐  ┌──────────┐     │
         │  │categoryA │  │categoryB │  │categoryC │     │
         │  │ regexes  │  │ regexes  │  │ regexes  │     │
         │  └──────────┘  └──────────┘  └──────────┘     │
         └────────────────────────────────────────────────┘

         ┌─ regex group 3 ────────────────────────────────┐
         │  ┌──────────┐  ┌──────────┐  ┌──────────┐     │
         │  │categoryA │  │categoryB │  │categoryC │     │
         │  │ regexes  │  │ regexes  │  │ regexes  │     │
         │  └──────────┘  └──────────┘  └──────────┘     │
         └────────────────────────────────────────────────┘

all data point tags and fields ──────────────────────────────────────────────►
```

## Aggregation

When `aggregation_period` and `aggregation_measurement` are set, the plugin
emits classification counters as separate metrics at the end of each period.
Three independent aggregation types can be enabled simultaneously:

- **Summary**: one metric per period with a fixed tag, counting all categories.
- **By group**: one metric per active regex group per period.
- **By selector**: one metric per observed selector value per period.

Aggregation metrics use the measurement name from `aggregation_measurement`.
Data points with all-zero fields are suppressed unless `aggregation_includes_zeroes`
is enabled.

## Example output

```text
# Passthrough metric with result tag added:
syslog,host=pg123,severity=warning message="WARNING: disk full" 1700000000

# Summary aggregation after one period:
status,summary=full okay=12i,warning=3i,critical=1i,dropped=2i,total=18i 1700000300

# Per-group aggregation:
status,host_type=database okay=8i,warning=2i,total=10i 1700000300
status,host_type=firewall warning=1i,critical=1i,total=8i 1700000300
```

## Configuration

```toml @sample.conf
```

## Options

### Selector options

| Option | Description |
| --- | --- |
| `selector_tag` | Tag whose value selects the regex group. Mutually exclusive with `selector_field`. |
| `selector_field` | Field whose value selects the regex group. |
| `selector_mapping` | Ordered list of `{regex: group_name}` elements. Use `"*"` as the group name to pass the selector value through unchanged. |
| `default_regex_group` | Regex group to use when no selector mapping entry matches. |

### Classification options

| Option | Description |
| --- | --- |
| `match_tag` | Tag whose value is matched against category regexes. Exactly one of `match_tag`/`match_field` is required. |
| `match_field` | Field whose value is matched against category regexes. |
| `mapped_selector_regexes` | TOML table mapping each group name to an ordered list of `{category: regex}` entries. Category values may be a single regex string, a multi-line string (one trimmed regex per non-blank line), or an array of strings. |
| `default_category` | Category to apply when no regex matches. Metrics are dropped if unset and no match is found. |
| `drop_categories` | Category name or list of names whose matched metrics are dropped after classification. |
| `result_tag` | Tag to set to the matched category name. Exactly one of `result_tag`/`result_field` is required. |
| `result_field` | Field to set to the matched category name. |

### Aggregation options

| Option | Description |
| --- | --- |
| `aggregation_period` | How often to emit aggregation metrics (e.g. `"5m"`). Must be ≥ 1s. |
| `aggregation_measurement` | Measurement name for aggregation metrics. |
| `aggregation_dropped_field` | Field name for the count of dropped metrics. |
| `aggregation_total_field` | Field name for the total count of all metrics processed. |
| `aggregation_summary_tag` | Tag name for summary aggregation. |
| `aggregation_summary_value` | Tag value for summary aggregation. |
| `aggregation_summary_fields` | Fields to include in summary aggregation metrics. |
| `aggregation_group_tag` | Tag name for per-group aggregation. |
| `aggregation_group_fields` | Fields to include in per-group aggregation metrics. |
| `aggregation_selector_tag` | Tag name for per-selector aggregation. |
| `aggregation_selector_fields` | Fields to include in per-selector aggregation metrics. |
| `aggregation_includes_zeroes` | Include fields with zero counts in aggregation output. Default: `false`. |
