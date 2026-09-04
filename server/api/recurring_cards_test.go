package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/focalboard/server/model"

	"github.com/mattermost/mattermost/server/public/shared/mlog"
)

// These cover only what the handler itself decides, before any application or
// permission machinery is reached: that a malformed payload is the caller's fault
// and is reported as such. Everything past that point — validation, the phase
// anchor, the ordering of the two writes — is covered at the app layer, where it
// can be driven with a mock store instead of a whole server.

func recurrenceRequest(t *testing.T, method, body string) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()

	request := httptest.NewRequest(method, "/boards/board-id/cards/card-id/recurrence", bytes.NewBufferString(body))
	request = mux.SetURLVars(request, map[string]string{"boardID": "board-id", "cardID": "card-id"})

	return request, httptest.NewRecorder()
}

func TestHandleSetCardRecurrenceRejectsBadPayload(t *testing.T) {
	testAPI := API{logger: mlog.CreateConsoleTestLogger(t)}

	t.Run("malformed JSON is a bad request", func(t *testing.T) {
		request, response := recurrenceRequest(t, http.MethodPut, "{not json")

		testAPI.handleSetCardRecurrence(response, request)

		result := response.Result()
		defer result.Body.Close()

		require.Equal(t, http.StatusBadRequest, result.StatusCode)
	})

	t.Run("a null configuration is a bad request rather than a panic", func(t *testing.T) {
		request, response := recurrenceRequest(t, http.MethodPut, "null")

		testAPI.handleSetCardRecurrence(response, request)

		result := response.Result()
		defer result.Body.Close()

		require.Equal(t, http.StatusBadRequest, result.StatusCode)

		body, err := io.ReadAll(result.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "recurrence configuration is required")
	})
}

func TestHandlePreviewCardRecurrenceRejectsBadPayload(t *testing.T) {
	testAPI := API{logger: mlog.CreateConsoleTestLogger(t)}

	t.Run("malformed JSON is a bad request", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost,
			"/boards/board-id/cards/card-id/recurrence/preview", bytes.NewBufferString("{"))
		request = mux.SetURLVars(request, map[string]string{"boardID": "board-id", "cardID": "card-id"})
		response := httptest.NewRecorder()

		testAPI.handlePreviewCardRecurrence(response, request)

		result := response.Result()
		defer result.Body.Close()

		require.Equal(t, http.StatusBadRequest, result.StatusCode)
	})
}

// TestRecurrencePreviewSerialises pins the wire shape the settings form depends on:
// a null next run has to survive as null rather than becoming zero, because zero is
// a real instant and would read as "due since 1970".
func TestRecurrencePreviewSerialises(t *testing.T) {
	t.Run("a null next run stays null", func(t *testing.T) {
		data, err := json.Marshal(&model.RecurrencePreview{Valid: true})
		require.NoError(t, err)
		require.Contains(t, string(data), `"nextRunAt":null`)
	})

	t.Run("problems carry a field and a reason", func(t *testing.T) {
		preview := &model.RecurrencePreview{
			Valid: false,
			Problems: []model.ErrInvalidRecurrence{
				{Field: model.RecurrenceFieldTimezone, Reason: "is not a known IANA timezone name"},
			},
		}

		data, err := json.Marshal(preview)
		require.NoError(t, err)

		var decoded model.RecurrencePreview
		require.NoError(t, json.Unmarshal(data, &decoded))
		require.Len(t, decoded.Problems, 1)
		require.Equal(t, model.RecurrenceFieldTimezone, decoded.Problems[0].Field)
		require.NotEmpty(t, decoded.Problems[0].Reason)
	})
}
