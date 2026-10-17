package qb

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetDateExpression(t *testing.T) {
	tests := []struct {
		name          string
		dialect       string
		dateExpr      string
		timezone      string
		offsetSeconds int
		wantContains  []string
	}{
		{
			name:          "sqlite dialect",
			dialect:       "sqlite",
			dateExpr:      "se.created_at",
			timezone:      "UTC",
			offsetSeconds: 0,
			wantContains:  []string{"strftime", "%Y-%m-%d", "se.created_at"},
		},
		{
			name:          "sqlite3 dialect",
			dialect:       "sqlite3",
			dateExpr:      "se.created_at",
			timezone:      "America/New_York",
			offsetSeconds: -14400,
			wantContains:  []string{"strftime", "%Y-%m-%d", "se.created_at", "-14400"},
		},
		{
			name:          "mysql dialect",
			dialect:       "mysql",
			dateExpr:      "se.created_at",
			timezone:      "America/New_York",
			offsetSeconds: -14400,
			wantContains:  []string{"DATE_FORMAT", "CONVERT_TZ", "-04:00", "%Y-%m-%d"},
		},
		{
			name:          "postgres dialect",
			dialect:       "postgres",
			dateExpr:      "se.created_at",
			timezone:      "UTC",
			offsetSeconds: 0,
			wantContains:  []string{"to_char", "AT TIME ZONE", "YYYY-MM-DD"},
		},
		{
			name:          "postgresql dialect",
			dialect:       "postgresql",
			dateExpr:      "se.created_at",
			timezone:      "Europe/London",
			offsetSeconds: 0,
			wantContains:  []string{"to_char", "Europe/London"},
		},
		{
			name:          "unknown dialect falls back to DATE()",
			dialect:       "oracle",
			dateExpr:      "se.created_at",
			timezone:      "UTC",
			offsetSeconds: 0,
			wantContains:  []string{"DATE(se.created_at)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getDateExpression(tt.dialect, tt.dateExpr, tt.timezone, tt.offsetSeconds)
			for _, want := range tt.wantContains {
				assert.Contains(t, got, want, "date expression should contain %q", want)
			}
		})
	}
}

func TestAllowedDailyQueryConfigs(t *testing.T) {
	assert.Contains(t, AllowedDailyQueryConfigs, DailyThroughputByChannel, "should have config for ByChannel")
	assert.Contains(t, AllowedDailyQueryConfigs, DailyThroughputByModel, "should have config for ByModel")

	channelConfig := AllowedDailyQueryConfigs[DailyThroughputByChannel]
	assert.Contains(t, channelConfig.IDColumn, "channel_id", "should include channel_id")
	// date is added separately via dateExpr in GROUP BY clauses

	modelConfig := AllowedDailyQueryConfigs[DailyThroughputByModel]
	assert.Contains(t, modelConfig.IDColumn, "model_id", "should include model_id")
	// date is added separately via dateExpr in GROUP BY clauses
}

func TestDailyThroughputQueryTypeEnum(t *testing.T) {
	assert.Equal(t, DailyThroughputQueryType(0), DailyThroughputByChannel, "DailyThroughputByChannel should be 0")
	assert.Equal(t, DailyThroughputQueryType(1), DailyThroughputByModel, "DailyThroughputByModel should be 1")
}

func TestBuildDailyPerformanceStatsQuery(t *testing.T) {
	tests := []struct {
		name            string
		dialect         string
		timezone        string
		offsetSeconds   int
		queryType       DailyThroughputQueryType
		placeholder     string
		mode            ThroughputQueryMode
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:          "model query with postgres includes requests join",
			dialect:       "postgres",
			timezone:      "UTC",
			offsetSeconds: 0,
			queryType:     DailyThroughputByModel,
			placeholder:   "$1",
			mode:          ThroughputModeRowNumber,
			wantContains: []string{
				"WITH successful_execs AS",
				"JOIN requests r ON se.request_id = r.id",
				"r.model_id",
				"to_char",
				"AT TIME ZONE",
				"NULLIF(",
				", 0)",
				"GROUP BY exec_date",
				"WHERE daily.throughput IS NOT NULL",
				"AND daily.throughput > 0",
				"ORDER BY date DESC, throughput DESC",
			},
			wantNotContains: []string{},
		},
		{
			name:          "channel query with postgres excludes requests join",
			dialect:       "postgres",
			timezone:      "UTC",
			offsetSeconds: 0,
			queryType:     DailyThroughputByChannel,
			placeholder:   "$1",
			mode:          ThroughputModeRowNumber,
			wantContains: []string{
				"WITH successful_execs AS",
				"se.channel_id",
				"to_char",
				"AT TIME ZONE",
				"NULLIF(",
				", 0)",
				"GROUP BY exec_date",
				"WHERE daily.throughput IS NOT NULL",
				"AND daily.throughput > 0",
				"ORDER BY date DESC, throughput DESC",
			},
			wantNotContains: []string{
				"JOIN requests r ON se.request_id = r.id",
				"r.model_id",
			},
		},
		{
			name:          "model query with mysql",
			dialect:       "mysql",
			timezone:      "America/New_York",
			offsetSeconds: -14400,
			queryType:     DailyThroughputByModel,
			placeholder:   "?",
			mode:          ThroughputModeRowNumber,
			wantContains: []string{
				"WITH successful_execs AS",
				"JOIN requests r ON se.request_id = r.id",
				"r.model_id",
				"DATE_FORMAT",
				"CONVERT_TZ",
				"-04:00",
				"NULLIF(",
				", 0)",
				"GROUP BY exec_date",
				"WHERE daily.throughput IS NOT NULL",
				"AND daily.throughput > 0",
			},
		},
		{
			name:          "channel query with mysql",
			dialect:       "mysql",
			timezone:      "UTC",
			offsetSeconds: 0,
			queryType:     DailyThroughputByChannel,
			placeholder:   "?",
			mode:          ThroughputModeRowNumber,
			wantContains: []string{
				"se.channel_id",
				"DATE_FORMAT",
				"CONVERT_TZ",
				"NULLIF(",
				", 0)",
				"GROUP BY exec_date",
				"WHERE daily.throughput IS NOT NULL",
				"AND daily.throughput > 0",
			},
			wantNotContains: []string{
				"JOIN requests r ON se.request_id = r.id",
			},
		},
		{
			name:          "model query with sqlite",
			dialect:       "sqlite",
			timezone:      "UTC",
			offsetSeconds: 0,
			queryType:     DailyThroughputByModel,
			placeholder:   "?",
			mode:          ThroughputModeRowNumber,
			wantContains: []string{
				"WITH successful_execs AS",
				"JOIN requests r ON se.request_id = r.id",
				"r.model_id",
				"strftime",
				"NULLIF(",
				", 0)",
				"GROUP BY exec_date",
				"WHERE daily.throughput IS NOT NULL",
				"AND daily.throughput > 0",
			},
		},
		{
			name:          "channel query with sqlite3",
			dialect:       "sqlite3",
			timezone:      "UTC",
			offsetSeconds: 0,
			queryType:     DailyThroughputByChannel,
			placeholder:   "?",
			mode:          ThroughputModeRowNumber,
			wantContains: []string{
				"se.channel_id",
				"strftime",
				"NULLIF(",
				", 0)",
				"GROUP BY exec_date",
				"WHERE daily.throughput IS NOT NULL",
				"AND daily.throughput > 0",
			},
			wantNotContains: []string{
				"JOIN requests r ON se.request_id = r.id",
			},
		},
		{
			name:          "model query with sqlite3 and MaxID mode",
			dialect:       "sqlite3",
			timezone:      "UTC",
			offsetSeconds: 0,
			queryType:     DailyThroughputByModel,
			placeholder:   "?",
			mode:          ThroughputModeMaxID,
			wantContains: []string{
				"MAX(se2.id)",
				"latest_execs",
				"JOIN requests r ON se.request_id = r.id",
				"r.model_id",
				"strftime",
			},
			wantNotContains: []string{
				"successful_execs",
				"ROW_NUMBER() OVER (PARTITION BY request_id",
			},
		},
		{
			name:          "model query with postgresql dialect alias",
			dialect:       "postgresql",
			timezone:      "Europe/London",
			offsetSeconds: 0,
			queryType:     DailyThroughputByModel,
			placeholder:   "$1",
			mode:          ThroughputModeRowNumber,
			wantContains: []string{
				"AT TIME ZONE 'Europe/London'",
				"JOIN requests r ON se.request_id = r.id",
				"r.model_id",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildDailyPerformanceStatsQuery(tt.dialect, tt.timezone, tt.offsetSeconds, tt.queryType, tt.placeholder, tt.mode)

			for _, want := range tt.wantContains {
				assert.Contains(t, got, want, "query should contain %q", want)
			}

			for _, notWant := range tt.wantNotContains {
				assert.NotContains(t, got, notWant, "query should not contain %q", notWant)
			}
		})
	}
}

func TestBuildDailyPerformanceStatsQuery_TTFTCalculation(t *testing.T) {
	got := BuildDailyPerformanceStatsQuery("postgres", "UTC", 0, DailyThroughputByModel, "$1", ThroughputModeRowNumber)

	assert.Contains(t, got, "NULLIF(", "should use NULLIF for TTFT denominator")
	assert.Contains(t, got, ", 0)", "should have NULLIF(..., 0) pattern")
	assert.Contains(t, got, "metrics_first_token_latency_ms", "should reference first token latency")
	assert.Contains(t, got, "SUM(CASE", "should use CASE for TTFT calculation")
}

func TestBuildDailyPerformanceStatsQuery_GroupByBehavior(t *testing.T) {
	tests := []struct {
		name      string
		queryType DailyThroughputQueryType
		wantIDCol string
	}{
		{
			name:      "model query groups by model_id",
			queryType: DailyThroughputByModel,
			wantIDCol: "model_id",
		},
		{
			name:      "channel query groups by channel_id",
			queryType: DailyThroughputByChannel,
			wantIDCol: "channel_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildDailyPerformanceStatsQuery("postgres", "UTC", 0, tt.queryType, "$1", ThroughputModeRowNumber)

			assert.Contains(t, got, "GROUP BY exec_date", "should group by CTE column exec_date")

			assert.Contains(t, got, tt.wantIDCol, "should group by "+tt.wantIDCol)
		})
	}
}

func TestBuildDailyPerformanceStatsQuery_PostFilter(t *testing.T) {
	got := BuildDailyPerformanceStatsQuery("postgres", "UTC", 0, DailyThroughputByModel, "$1", ThroughputModeRowNumber)

	assert.Contains(t, got, "WHERE daily.throughput IS NOT NULL", "should filter null throughput")
	assert.Contains(t, got, "AND daily.throughput > 0", "should filter zero/negative throughput")
}

func TestBuildDailyPerformanceStatsQuery_ConditionalJoinRequests(t *testing.T) {
	modelQuery := BuildDailyPerformanceStatsQuery("postgres", "UTC", 0, DailyThroughputByModel, "$1", ThroughputModeRowNumber)
	assert.Contains(t, modelQuery, "JOIN requests r ON se.request_id = r.id", "model query should join requests")
	assert.Contains(t, modelQuery, "r.model_id", "model query should reference r.model_id")

	channelQuery := BuildDailyPerformanceStatsQuery("postgres", "UTC", 0, DailyThroughputByChannel, "$1", ThroughputModeRowNumber)
	assert.NotContains(t, channelQuery, "JOIN requests r ON se.request_id = r.id", "channel query should not join requests")
	assert.NotContains(t, channelQuery, "r.model_id", "channel query should not reference r.model_id")
}
