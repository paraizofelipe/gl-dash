package data

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCurrentLoginName(t *testing.T) {
	t.Run("returns the username from a successful query", func(t *testing.T) {
		defer SetClient(nil)

		mockClient := newMockGraphQLClient(
			t,
			staticJSONHandler(http.StatusOK, `{"data":{"currentUser":{"username":"jdoe"}}}`),
		)
		SetClient(mockClient)

		login, err := CurrentLoginName()
		require.NoError(t, err)
		require.Equal(t, "jdoe", login)
	})

	t.Run("propagates the error instead of silently returning an empty login", func(t *testing.T) {
		defer SetClient(nil)

		mockClient := newMockGraphQLClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		SetClient(mockClient)

		login, err := CurrentLoginName()
		require.Error(t, err)
		require.Equal(t, "", login)
	})

	t.Run("propagates graphql errors from the response body", func(t *testing.T) {
		defer SetClient(nil)

		mockClient := newMockGraphQLClient(
			t,
			staticJSONHandler(http.StatusOK, `{"data":null,"errors":[{"message":"unauthorized"}]}`),
		)
		SetClient(mockClient)

		login, err := CurrentLoginName()
		require.Error(t, err)
		require.Equal(t, "", login)
	})
}
