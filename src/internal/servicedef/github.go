package servicedef

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// repoURL format: "github.com/org/repo" or "https://github.com/org/repo"
func fetchServiceYAML(ctx context.Context, client *http.Client, repoURL, githubToken string) (string, error) {
	owner, repo, err := parseGitHubRepo(repoURL)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/.titlis/service.yaml", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+githubToken)
	req.Header.Set("Accept", "application/vnd.github.v3.raw")
	req.Header.Set("User-Agent", "titlis-operator-go")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("github fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github returned %d for %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	return string(body), nil
}

func parseGitHubRepo(repoURL string) (owner, repo string, err error) {
	u := strings.TrimPrefix(repoURL, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "github.com/")
	parts := strings.SplitN(u, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid github repo URL: %q (expected github.com/owner/repo)", repoURL)
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}
