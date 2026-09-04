package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mattermost/focalboard/server/model"
	"github.com/mattermost/focalboard/server/services/audit"

	"github.com/mattermost/mattermost/server/public/shared/mlog"
)

func (a *API) registerRecurringCardsRoutes(r *mux.Router) {
	// Recurring cards APIs
	r.HandleFunc("/boards/{boardID}/cards/{cardID}/recurrence", a.sessionRequired(a.handleSetCardRecurrence)).Methods("PUT")
	r.HandleFunc("/boards/{boardID}/cards/{cardID}/recurrence", a.sessionRequired(a.handleDeleteCardRecurrence)).Methods("DELETE")
	r.HandleFunc("/boards/{boardID}/cards/{cardID}/recurrence/preview", a.sessionRequired(a.handlePreviewCardRecurrence)).Methods("POST")
}

// readRecurrenceRequest performs the checks the three handlers share: the payload
// parses, the card exists, it really is a card, it belongs to the board in the
// path, and the caller may edit that board.
//
// It writes the response itself on failure, so callers return as soon as ok is
// false.
func (a *API) readRecurrenceRequest(w http.ResponseWriter, r *http.Request, withBody bool) (card *model.Block, cfg *model.RecurrenceConfig, ok bool) {
	userID := getUserID(r)
	boardID := mux.Vars(r)["boardID"]
	cardID := mux.Vars(r)["cardID"]

	if withBody {
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			a.errorResponse(w, r, err)
			return nil, nil, false
		}

		if err := json.Unmarshal(requestBody, &cfg); err != nil {
			a.errorResponse(w, r, model.NewErrBadRequest(err.Error()))
			return nil, nil, false
		}

		if cfg == nil {
			a.errorResponse(w, r, model.NewErrBadRequest("a recurrence configuration is required"))
			return nil, nil, false
		}
	}

	card, err := a.app.GetBlockByID(cardID)
	if err != nil {
		a.errorResponse(w, r, err)
		return nil, nil, false
	}

	if card.Type != model.TypeCard {
		a.errorResponse(w, r, model.NewErrBadRequest("block "+cardID+" is not a card"))
		return nil, nil, false
	}

	// The board is in the path as well as on the card, so a mismatch is a malformed
	// request rather than something to silently work around.
	if card.BoardID != boardID {
		a.errorResponse(w, r, model.ErrBoardIDMismatch)
		return nil, nil, false
	}

	if !a.permissions.HasPermissionToBoard(userID, boardID, model.PermissionManageBoardCards) {
		a.errorResponse(w, r, model.NewErrPermission("access denied to change the recurrence of a card"))
		return nil, nil, false
	}

	return card, cfg, true
}

func (a *API) handleSetCardRecurrence(w http.ResponseWriter, r *http.Request) {
	// swagger:operation PUT /boards/{boardID}/cards/{cardID}/recurrence setCardRecurrence
	//
	// Creates or replaces the recurrence configuration of a card.
	//
	// ---
	// produces:
	// - application/json
	// parameters:
	// - name: boardID
	//   in: path
	//   description: Board ID
	//   required: true
	//   type: string
	// - name: cardID
	//   in: path
	//   description: Card ID
	//   required: true
	//   type: string
	// - name: Body
	//   in: body
	//   description: the recurrence configuration
	//   required: true
	//   schema:
	//     "$ref": "#/definitions/RecurrenceConfig"
	// security:
	// - BearerAuth: []
	// responses:
	//   '200':
	//     description: success
	//     schema:
	//       $ref: '#/definitions/RecurringCard'
	//   default:
	//     description: internal error
	//     schema:
	//       "$ref": "#/definitions/ErrorResponse"

	userID := getUserID(r)

	card, cfg, ok := a.readRecurrenceRequest(w, r, true)
	if !ok {
		return
	}

	auditRec := a.makeAuditRecord(r, "setCardRecurrence", audit.Fail)
	defer a.audit.LogRecord(audit.LevelModify, auditRec)
	auditRec.AddMeta("boardID", card.BoardID)
	auditRec.AddMeta("cardID", card.ID)

	recurringCard, err := a.app.SetCardRecurrence(card.ID, cfg, userID)
	if err != nil {
		// A configuration the validator refuses is the caller's mistake, and its
		// message names every field that is wrong.
		var invalid model.ErrRecurrenceInvalid
		if errors.As(err, &invalid) {
			a.errorResponse(w, r, model.NewErrBadRequest(invalid.Error()))
			return
		}
		a.errorResponse(w, r, err)
		return
	}

	a.logger.Debug("SetCardRecurrence",
		mlog.String("boardID", card.BoardID),
		mlog.String("cardID", card.ID),
		mlog.String("userID", userID),
	)

	data, err := json.Marshal(recurringCard)
	if err != nil {
		a.errorResponse(w, r, err)
		return
	}

	jsonBytesResponse(w, http.StatusOK, data)

	auditRec.Success()
}

func (a *API) handleDeleteCardRecurrence(w http.ResponseWriter, r *http.Request) {
	// swagger:operation DELETE /boards/{boardID}/cards/{cardID}/recurrence deleteCardRecurrence
	//
	// Stops a card recurring. The configuration is kept on the card so that turning
	// the recurrence back on restores it.
	//
	// ---
	// produces:
	// - application/json
	// parameters:
	// - name: boardID
	//   in: path
	//   description: Board ID
	//   required: true
	//   type: string
	// - name: cardID
	//   in: path
	//   description: Card ID
	//   required: true
	//   type: string
	// security:
	// - BearerAuth: []
	// responses:
	//   '200':
	//     description: success
	//   default:
	//     description: internal error
	//     schema:
	//       "$ref": "#/definitions/ErrorResponse"

	userID := getUserID(r)

	card, _, ok := a.readRecurrenceRequest(w, r, false)
	if !ok {
		return
	}

	auditRec := a.makeAuditRecord(r, "deleteCardRecurrence", audit.Fail)
	defer a.audit.LogRecord(audit.LevelModify, auditRec)
	auditRec.AddMeta("boardID", card.BoardID)
	auditRec.AddMeta("cardID", card.ID)

	if err := a.app.DeleteCardRecurrence(card.ID, userID); err != nil {
		a.errorResponse(w, r, err)
		return
	}

	a.logger.Debug("DeleteCardRecurrence",
		mlog.String("boardID", card.BoardID),
		mlog.String("cardID", card.ID),
		mlog.String("userID", userID),
	)

	jsonStringResponse(w, http.StatusOK, "{}")

	auditRec.Success()
}

func (a *API) handlePreviewCardRecurrence(w http.ResponseWriter, r *http.Request) {
	// swagger:operation POST /boards/{boardID}/cards/{cardID}/recurrence/preview previewCardRecurrence
	//
	// Reports what saving a recurrence configuration would do, without saving it.
	// Returns the computed next occurrence and every validation problem, so that a
	// settings form can drive both its preview and its save button from one call.
	//
	// ---
	// produces:
	// - application/json
	// parameters:
	// - name: boardID
	//   in: path
	//   description: Board ID
	//   required: true
	//   type: string
	// - name: cardID
	//   in: path
	//   description: Card ID
	//   required: true
	//   type: string
	// - name: Body
	//   in: body
	//   description: the recurrence configuration to check
	//   required: true
	//   schema:
	//     "$ref": "#/definitions/RecurrenceConfig"
	// security:
	// - BearerAuth: []
	// responses:
	//   '200':
	//     description: success
	//     schema:
	//       $ref: '#/definitions/RecurrencePreview'
	//   default:
	//     description: internal error
	//     schema:
	//       "$ref": "#/definitions/ErrorResponse"

	card, cfg, ok := a.readRecurrenceRequest(w, r, true)
	if !ok {
		return
	}

	auditRec := a.makeAuditRecord(r, "previewCardRecurrence", audit.Fail)
	defer a.audit.LogRecord(audit.LevelRead, auditRec)
	auditRec.AddMeta("boardID", card.BoardID)
	auditRec.AddMeta("cardID", card.ID)

	// An invalid configuration is the answer here, not a failure: reporting what is
	// wrong with it is the entire purpose of the call.
	preview, err := a.app.PreviewCardRecurrence(card.ID, cfg)
	if err != nil {
		a.errorResponse(w, r, err)
		return
	}

	data, err := json.Marshal(preview)
	if err != nil {
		a.errorResponse(w, r, err)
		return
	}

	jsonBytesResponse(w, http.StatusOK, data)

	auditRec.Success()
}
