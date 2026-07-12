package data

import "regexp"

type TranslatedQuery struct {
	ProjectPath       string
	State             string
	AuthorUsername    string
	NotAuthorUsername string
	AssigneeUsername  string
	ReviewerUsername  string
	Labels            []string
	SourceBranch      string
	OrderBy           string
	Sort              string
	Unsupported       []string
}

var qualifierRegex = regexp.MustCompile(`(-?[a-z-]+):(\S+)`)

func TranslateSearchQuery(query, currentUsername string) TranslatedQuery {
	t := TranslatedQuery{OrderBy: "updated_at", Sort: "desc"}
	resolveMe := func(v string) string {
		if v == "@me" {
			return currentUsername
		}
		return v
	}
	for _, m := range qualifierRegex.FindAllStringSubmatch(query, -1) {
		key, val := m[1], m[2]
		switch key {
		case "is":
			switch val {
			case "open":
				t.State = "opened"
			case "closed":
				t.State = "closed"
			case "merged":
				t.State = "merged"
			}
		case "author":
			t.AuthorUsername = resolveMe(val)
		case "-author":
			t.NotAuthorUsername = resolveMe(val)
		case "assignee":
			t.AssigneeUsername = resolveMe(val)
		case "review-requested":
			t.ReviewerUsername = resolveMe(val)
		case "label":
			t.Labels = append(t.Labels, val)
		case "head":
			t.SourceBranch = val
		case "project", "repo":
			t.ProjectPath = val
		case "involves":
			t.Unsupported = append(t.Unsupported, "involves:"+resolveMe(val))
		case "owner":
			t.Unsupported = append(t.Unsupported, "owner:"+val)
		case "updated":
			t.Unsupported = append(t.Unsupported, "updated:"+val)
		case "archived", "sort":
		}
	}
	return t
}
