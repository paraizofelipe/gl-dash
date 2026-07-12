package data

import (
	"strings"
	"sync"

	"charm.land/log/v2"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"
)

var (
	repoLabelCache = make(map[string][]Label)
	labelCacheMu   sync.RWMutex
)

func CachedRepoLabels(repoNameWithOwner string) ([]Label, bool) {
	labelCacheMu.RLock()
	defer labelCacheMu.RUnlock()
	labels, ok := repoLabelCache[repoNameWithOwner]
	return labels, ok
}

func FetchRepoLabels(repoNameWithOwner string) ([]Label, error) {
	if cachedLabels, ok := CachedRepoLabels(repoNameWithOwner); ok {
		return cachedLabels, nil
	}

	log.Debug("Fetching repo labels", "repoNameWithOwner", repoNameWithOwner)

	glLabels, err := listProjectLabels(repoNameWithOwner)
	if err != nil {
		return nil, err
	}

	filteredLabels := make([]Label, 0, len(glLabels))
	for _, l := range glLabels {
		if strings.TrimSpace(l.Name) != "" {
			filteredLabels = append(filteredLabels, Label{
				Name:        l.Name,
				Color:       l.Color,
				Description: l.Description,
			})
		}
	}

	labelCacheMu.Lock()
	defer labelCacheMu.Unlock()

	if labels, ok := repoLabelCache[repoNameWithOwner]; ok {
		return labels, nil
	}

	repoLabelCache[repoNameWithOwner] = filteredLabels
	log.Debug(
		"Successfully fetched repo labels",
		"repoNameWithOwner",
		repoNameWithOwner,
		"len",
		len(filteredLabels),
	)
	return filteredLabels, nil
}

func listProjectLabels(projectPath string) ([]*gitlabapi.Label, error) {
	c, err := resolveRESTClient()
	if err != nil {
		return nil, err
	}

	opts := &gitlabapi.ListLabelsOptions{
		ListOptions: gitlabapi.ListOptions{PerPage: 100, Page: 1},
	}

	var all []*gitlabapi.Label
	for {
		labels, resp, err := c.Labels.ListLabels(projectPath, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, labels...)
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

func ClearLabelCache() {
	labelCacheMu.Lock()
	defer labelCacheMu.Unlock()
	repoLabelCache = make(map[string][]Label)
}

func ClearRepoLabelCache(repoNameWithOwner string) {
	labelCacheMu.Lock()
	defer labelCacheMu.Unlock()
	delete(repoLabelCache, repoNameWithOwner)
}

func LabelNames(labels []Label) []string {
	names := make([]string, len(labels))
	for i, label := range labels {
		names[i] = label.Name
	}
	return names
}
