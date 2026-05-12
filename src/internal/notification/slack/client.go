package slack

import (
	"context"
	"time"

	slackgo "github.com/slack-go/slack"
	"golang.org/x/time/rate"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/titlis/operator/internal/config"
	"github.com/titlis/operator/internal/model"
	"github.com/titlis/operator/internal/notification"
)

type Client struct {
	api            *slackgo.Client
	webhookURL     string
	defaultChannel string
	perMinute      *rate.Limiter
	perHour        *rate.Limiter
	maxMsgLen      int
	maxRetries     int
	timeout        time.Duration
}

func NewClient(cfg *config.Settings) *Client {
	var api *slackgo.Client
	if cfg.SlackBotToken != "" {
		api = slackgo.New(cfg.SlackBotToken)
	}
	return &Client{
		api:            api,
		webhookURL:     cfg.SlackWebhookURL,
		defaultChannel: cfg.SlackDefaultChannel,
		perMinute:      rate.NewLimiter(rate.Every(time.Minute/time.Duration(cfg.SlackRatePerMinute)), cfg.SlackRatePerMinute),
		perHour:        rate.NewLimiter(rate.Every(time.Hour/time.Duration(cfg.SlackRatePerHour)), cfg.SlackRatePerHour),
		maxMsgLen:      cfg.SlackMaxMsgLength,
		maxRetries:     cfg.SlackMaxRetries,
		timeout:        time.Duration(cfg.SlackTimeoutSeconds) * time.Second,
	}
}

func (c *Client) Send(ctx context.Context, channel, title, message string, sev notification.Severity) error {
	logger := log.FromContext(ctx)

	if !c.perMinute.Allow() || !c.perHour.Allow() {
		logger.Info("slack rate limit reached, dropping notification", "channel", channel)
		return nil
	}

	if len(message) > c.maxMsgLen {
		message = message[:c.maxMsgLen-3] + "..."
	}

	var lastErr error
	backoff := 2 * time.Second
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}
		if err := c.send(ctx, channel, title, message, sev); err != nil {
			lastErr = err
			logger.Info("slack send failed, retrying", "attempt", attempt, "error", err)
			continue
		}
		return nil
	}
	return lastErr
}

func (c *Client) send(_ context.Context, channel, title, message string, sev notification.Severity) error {
	color := colorFor(sev)
	attachment := slackgo.Attachment{
		Color:   color,
		Title:   title,
		Text:    message,
		Pretext: "",
	}

	if c.api != nil {
		if channel == "" {
			channel = c.defaultChannel
		}
		_, _, err := c.api.PostMessage(channel, slackgo.MsgOptionAttachments(attachment))
		return err
	}
	if c.webhookURL != "" {
		return slackgo.PostWebhook(c.webhookURL, &slackgo.WebhookMessage{
			Channel:     channel,
			Attachments: []slackgo.Attachment{attachment},
		})
	}
	return nil
}

func (c *Client) SendBatch(ctx context.Context, ns string,
	items []notification.BatchItem, sev notification.Severity) error {

	scorecards := make([]model.ResourceScorecard, 0, len(items))
	for _, item := range items {
		scorecards = append(scorecards, model.ResourceScorecard{
			ResourceName:  item.Name,
			OverallScore:  item.OverallScore,
			CriticalIssues: item.Critical,
			ErrorIssues:   item.Errors,
			WarningIssues: item.Warnings,
		})
	}
	title, msg, calcSev := formatDigest(ns, scorecards)
	_ = calcSev
	ch := c.opsChannel(sev)
	return c.Send(ctx, ch, title, msg, sev)
}

func (c *Client) opsChannel(sev notification.Severity) string {
	switch sev {
	case notification.SeverityCritical, notification.SeverityError:
		return "#titlis-alerts"
	default:
		return "#titlis-operational"
	}
}

func colorFor(sev notification.Severity) string {
	switch sev {
	case notification.SeverityCritical:
		return "#FF0000"
	case notification.SeverityError:
		return "#FF6600"
	case notification.SeverityWarning:
		return "#FFCC00"
	default:
		return "#36A64F"
	}
}
