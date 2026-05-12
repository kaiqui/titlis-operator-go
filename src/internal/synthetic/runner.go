package synthetic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/titlis/operator/internal/synthetic/checks"
)

// MetricSender abstracts how gauge values are sent to the metrics backend.
type MetricSender interface {
	SendGauge(name string, value float64, tags []string) error
}

// Run starts one goroutine per check. Blocks until ctx is cancelled.
func Run(ctx context.Context, siteChecks []BaseCheck, jsonValueChecks []JSONValueCheck,
	metrics MetricSender) {

	for _, c := range siteChecks {
		go runSiteHealthLoop(ctx, c, metrics)
	}
	for _, c := range jsonValueChecks {
		go runJSONValueLoop(ctx, c, metrics)
	}
	<-ctx.Done()
}

func runSiteHealthLoop(ctx context.Context, check BaseCheck, metrics MetricSender) {
	logger := log.FromContext(ctx)
	interval := time.Duration(check.IntervalSeconds) * time.Second
	if interval == 0 {
		interval = 60 * time.Second
	}
	timeout := time.Duration(check.TimeoutSeconds*float64(time.Second))
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tags := flatTags(check.Tags)
			tags = append(tags, "check:"+check.Name)
			start := time.Now()
			healthy, statusCode := siteHealth(ctx, check.URL, timeout)
			elapsed := time.Since(start).Milliseconds()

			healthVal := 0.0
			if healthy {
				healthVal = 1.0
			}
			if err := metrics.SendGauge("synthetic.site.health", healthVal, tags); err != nil {
				logger.Error(err, "synthetic: send gauge failed", "check", check.Name)
			}
			if err := metrics.SendGauge("synthetic.site.response_time_ms", float64(elapsed), tags); err != nil {
				logger.Error(err, "synthetic: send gauge failed", "check", check.Name)
			}
			logger.V(1).Info("synthetic site_health", "name", check.Name,
				"healthy", healthy, "status", statusCode, "elapsed_ms", elapsed)
		}
	}
}

func runJSONValueLoop(ctx context.Context, check JSONValueCheck, metrics MetricSender) {
	logger := log.FromContext(ctx)
	interval := time.Duration(check.IntervalSeconds) * time.Second
	if interval == 0 {
		interval = 60 * time.Second
	}
	timeout := time.Duration(check.TimeoutSeconds * float64(time.Second))
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tags := flatTags(check.Tags)
			tags = append(tags, "check:"+check.Name)

			val, ok := jsonValue(ctx, check.URL, check.JSONPath, timeout)
			if !ok {
				logger.Info("synthetic: json_value extraction failed — metric not sent",
					"check", check.Name, "path", check.JSONPath)
				continue
			}
			if err := metrics.SendGauge(check.MetricName, val, tags); err != nil {
				logger.Error(err, "synthetic: send gauge failed", "check", check.Name)
			}
			logger.V(1).Info("synthetic json_value", "name", check.Name,
				"metric", check.MetricName, "value", val)
		}
	}
}

// siteHealth performs a GET and returns (healthy, statusCode).
func siteHealth(ctx context.Context, url string, timeout time.Duration) (bool, int) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return false, 0
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, 0
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400, resp.StatusCode
}

// jsonValue fetches url, decodes JSON, and extracts the value at dotPath.
func jsonValue(ctx context.Context, url, dotPath string, timeout time.Duration) (float64, bool) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return 0, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()

	var data map[string]any
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		return 0, false
	}
	return checks.ExtractJSONPath(data, dotPath)
}

func flatTags(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, fmt.Sprintf("%s:%s", k, v))
	}
	return out
}
