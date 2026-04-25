package main

import (
	"net/http"
	"sync"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/pkg/errors"
)

// Plugin implements the interface expected by the Mattermost server.
type Plugin struct {
	plugin.MattermostPlugin

	configurationLock sync.RWMutex
	configuration     *Configuration

	botUserID string
}

func (p *Plugin) OnActivate() error {
	config := p.getConfiguration()
	if err := config.IsValid(); err != nil {
		p.API.LogWarn("Plugin configuration invalid, some features may be unavailable", "error", err.Error())
	}

	botID, appErr := p.API.EnsureBotUser(&model.Bot{
		Username:    "gitlabcr-bot",
		DisplayName: "GitLab Code Review",
		Description: "GitLab MR 알림 및 요약 봇",
	})
	if appErr != nil {
		p.API.LogWarn("Failed to ensure bot user, webhook notifications will use fallback", "error", appErr.Error())
	} else {
		p.botUserID = botID
	}

	if err := p.registerCommands(); err != nil {
		return errors.Wrap(err, "failed to register commands")
	}

	return nil
}

func (p *Plugin) OnDeactivate() error {
	return nil
}

func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/action/diff":
		p.handleDiffAction(w, r)
	case "/action/post-comment":
		p.handlePostCommentAction(w, r)
	case "/webhook":
		p.handleWebhook(w, r)
	default:
		http.NotFound(w, r)
	}
}
