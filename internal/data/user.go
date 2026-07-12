package data

import "context"

func CurrentLoginName() (string, error) {
	client, err := resolveGraphQLClient()
	if err != nil {
		return "", err
	}

	var query struct {
		CurrentUser struct {
			Username string
		}
	}
	err = client.Query(context.Background(), &query, nil)
	return query.CurrentUser.Username, err
}
