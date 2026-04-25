package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	pluginID        = "com.mattermost.gitlab-review"
	actionPathDiff  = "/action/diff"
	maxButtonsPerAttachment = 20
)

// handleDiffAction processes button click requests from Mattermost.
func (p *Plugin) handleDiffAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req model.PostActionIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeActionResponse(w, "잘못된 요청입니다.")
		return
	}

	projectPath, _ := req.Context["project_path"].(string)
	mrIDFloat, _ := req.Context["mr_id"].(float64)
	filePath, _ := req.Context["file_path"].(string)
	mrID := int(mrIDFloat)

	if projectPath == "" || filePath == "" || mrID == 0 {
		writeActionResponse(w, "요청 파라미터가 올바르지 않습니다.")
		return
	}

	config := p.getConfiguration()
	if err := config.IsValid(); err != nil {
		writeActionResponse(w, "플러그인이 설정되지 않았습니다.")
		return
	}

	glClient, err := newGitLabClient(config.GitLabURL, config.GitLabToken)
	if err != nil {
		writeActionResponse(w, "GitLab 연결 실패: "+err.Error())
		return
	}

	diffs, err := glClient.GetMergeRequestDiffs(projectPath, mrID)
	if err != nil {
		writeActionResponse(w, fmt.Sprintf("MR !%d diff 조회 실패: %s", mrID, err.Error()))
		return
	}

	writeActionResponse(w, formatFileDiff(diffs, filePath))
}

func writeActionResponse(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "application/json")
	resp := &model.PostActionIntegrationResponse{
		EphemeralText: text,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// buildFileButtons converts a list of MR diffs into clickable Mattermost button attachments.
// Files are split into groups of maxButtonsPerAttachment to respect Mattermost UI limits.
func buildFileButtons(diffs []*MRDiff, projectPath string, mrID int, siteURL string) []*model.SlackAttachment {
	if len(diffs) == 0 {
		return nil
	}

	actionURL := siteURL + "/plugins/" + pluginID + actionPathDiff
	totalFiles := len(diffs)
	totalPages := (totalFiles + maxButtonsPerAttachment - 1) / maxButtonsPerAttachment

	var attachments []*model.SlackAttachment

	for page := 0; page < totalPages; page++ {
		start := page * maxButtonsPerAttachment
		end := start + maxButtonsPerAttachment
		if end > totalFiles {
			end = totalFiles
		}
		chunk := diffs[start:end]

		actions := make([]*model.PostAction, 0, len(chunk))
		for _, d := range chunk {
			filePath := d.NewPath
			if d.IsDeleted {
				filePath = d.OldPath
			}

			label := filePath
			if d.IsNew {
				label = "[+] " + label
			} else if d.IsDeleted {
				label = "[-] " + label
			} else if d.IsRenamed {
				label = "[R] " + label
			}

			actions = append(actions, &model.PostAction{
				Name: label,
				Type: model.PostActionTypeButton,
				Integration: &model.PostActionIntegration{
					URL: actionURL,
					Context: map[string]interface{}{
						"project_path": projectPath,
						"mr_id":        mrID,
						"file_path":    filePath,
					},
				},
			})
		}

		title := fmt.Sprintf("변경된 파일 (%d개)", totalFiles)
		if totalPages > 1 {
			title = fmt.Sprintf("변경된 파일 (%d개) — %d/%d", totalFiles, page+1, totalPages)
		}

		attachments = append(attachments, &model.SlackAttachment{
			Title:   title,
			Actions: actions,
		})
	}

	return attachments
}
