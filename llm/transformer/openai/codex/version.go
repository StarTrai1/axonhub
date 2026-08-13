package codex

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
	codexVersionRefreshInterval = 6 * time.Hour
	codexVersionRefreshTimeout  = 10 * time.Second
	codexLatestReleaseURL       = "https://api.github.com/repos/openai/codex/releases/latest"
)

var codexVersionCache struct {
	value       atomic.Pointer[string]
	lastAttempt atomic.Int64
	refreshing  atomic.Bool
}

// currentCodexVersion returns immediately from local state. The first request
// after each refresh interval starts a non-blocking refresh, so GitHub latency
// never contributes to upstream TTFT.
func currentCodexVersion() string {
	version := codexDefaultVersion
	if cached := codexVersionCache.value.Load(); cached != nil {
		version = *cached
	}

	now := time.Now()
	lastAttempt := time.Unix(0, codexVersionCache.lastAttempt.Load())
	if now.Sub(lastAttempt) < codexVersionRefreshInterval || !codexVersionCache.refreshing.CompareAndSwap(false, true) {
		return version
	}
	codexVersionCache.lastAttempt.Store(now.UnixNano())

	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("codex version refresh panicked", slog.Any("panic", recovered))
			}
			codexVersionCache.refreshing.Store(false)
		}()

		ctx, cancel := context.WithTimeout(context.Background(), codexVersionRefreshTimeout)
		defer cancel()

		latest, err := fetchLatestCodexVersion(ctx, http.DefaultClient, codexLatestReleaseURL)
		if err != nil {
			slog.Debug("failed to refresh Codex client version", slog.Any("error", err))
			return
		}

		current := codexDefaultVersion
		if cached := codexVersionCache.value.Load(); cached != nil {
			current = *cached
		}
		if compareCodexVersions(latest, current) <= 0 {
			return
		}

		codexVersionCache.value.Store(&latest)
		slog.Info("refreshed Codex client version", slog.String("version", latest))
	}()

	return version
}

func codexUserAgent(version string) string {
	return CodexCLIOriginator + "/" + version
}

func codexVersionFromUserAgent(userAgent string) (string, bool) {
	product := strings.Fields(userAgent)
	if len(product) == 0 {
		return "", false
	}
	_, version, found := strings.Cut(product[0], "/")
	if !found {
		return "", false
	}
	if _, ok := parseStableCodexVersion(version); !ok {
		return "", false
	}
	return version, true
}

func fetchLatestCodexVersion(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create Codex release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "AxonHub-Codex-Version-Checker")

	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch Codex release: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch Codex release: status %d", response.StatusCode)
	}

	var release struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decode Codex release: %w", err)
	}
	if release.Draft || release.Prerelease || !strings.HasPrefix(release.TagName, "rust-v") {
		return "", fmt.Errorf("latest release is not a stable Codex CLI release")
	}

	version := strings.TrimPrefix(release.TagName, "rust-v")
	if _, ok := parseStableCodexVersion(version); !ok {
		return "", fmt.Errorf("invalid stable Codex version %q", version)
	}
	return version, nil
}

func compareCodexVersions(left, right string) int {
	l, lok := parseStableCodexVersion(left)
	r, rok := parseStableCodexVersion(right)
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

func parseStableCodexVersion(version string) ([3]int, bool) {
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
