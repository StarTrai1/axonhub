package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	claudeCodeVersionRefreshInterval = 6 * time.Hour
	claudeCodeVersionRefreshTimeout  = 10 * time.Second
	claudeCodeLatestReleaseURL       = "https://api.github.com/repos/anthropics/claude-code/releases/latest"
)

var claudeCodeVersionCache struct {
	value       atomic.Pointer[string]
	lastAttempt atomic.Int64
	refreshing  atomic.Bool
}

func currentClaudeCodeUserAgent() string {
	version := claudeCodeVersionFromUserAgent(UserAgent)
	if cached := claudeCodeVersionCache.value.Load(); cached != nil {
		version = *cached
	}

	now := time.Now()
	lastAttempt := time.Unix(0, claudeCodeVersionCache.lastAttempt.Load())
	if now.Sub(lastAttempt) < claudeCodeVersionRefreshInterval || !claudeCodeVersionCache.refreshing.CompareAndSwap(false, true) {
		return claudeCodeUserAgent(version)
	}
	claudeCodeVersionCache.lastAttempt.Store(now.UnixNano())

	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("Claude Code version refresh panicked", slog.Any("panic", recovered))
			}
			claudeCodeVersionCache.refreshing.Store(false)
		}()

		ctx, cancel := context.WithTimeout(context.Background(), claudeCodeVersionRefreshTimeout)
		defer cancel()

		latest, err := fetchLatestClaudeCodeVersion(ctx, http.DefaultClient, claudeCodeLatestReleaseURL)
		if err != nil {
			slog.Debug("failed to refresh Claude Code client version", slog.Any("error", err))
			return
		}

		current := claudeCodeVersionFromUserAgent(UserAgent)
		if cached := claudeCodeVersionCache.value.Load(); cached != nil {
			current = *cached
		}
		if compareClaudeCodeVersions(latest, current) <= 0 {
			return
		}

		claudeCodeVersionCache.value.Store(&latest)
		slog.Info("refreshed Claude Code client version", slog.String("version", latest))
	}()

	return claudeCodeUserAgent(version)
}

func claudeCodeUserAgent(version string) string {
	return "claude-cli/" + version + " (external, cli)"
}

func claudeCodeVersionFromUserAgent(userAgent string) string {
	product := strings.Fields(userAgent)
	if len(product) == 0 {
		return claudeCodeDefaultVersion
	}
	_, version, found := strings.Cut(product[0], "/")
	if !found {
		return claudeCodeDefaultVersion
	}
	if _, ok := parseStableClaudeCodeVersion(version); !ok {
		return claudeCodeDefaultVersion
	}

	return version
}

func fetchLatestClaudeCodeVersion(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create Claude Code release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "AxonHub-Claude-Code-Version-Checker")

	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch Claude Code release: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch Claude Code release: status %d", response.StatusCode)
	}

	var release struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decode Claude Code release: %w", err)
	}
	if release.Draft || release.Prerelease || !strings.HasPrefix(release.TagName, "v") {
		return "", fmt.Errorf("latest release is not a stable Claude Code release")
	}

	version := strings.TrimPrefix(release.TagName, "v")
	if _, ok := parseStableClaudeCodeVersion(version); !ok {
		return "", fmt.Errorf("invalid stable Claude Code version %q", version)
	}

	return version, nil
}

func compareClaudeCodeVersions(left, right string) int {
	l, lok := parseStableClaudeCodeVersion(left)
	r, rok := parseStableClaudeCodeVersion(right)
	if !lok || !rok {
		return 0
	}
	for i := range l {
		if l[i] < r[i] {
			return -1
		}
		if l[i] > r[i] {
			return 1
		}
	}

	return 0
}

func parseStableClaudeCodeVersion(version string) ([3]int, bool) {
	var parsed [3]int
	parts := strings.Split(version, ".")
	if len(parts) != len(parsed) {
		return parsed, false
	}
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || strconv.Itoa(value) != part {
			return parsed, false
		}
		parsed[i] = value
	}

	return parsed, true
}
