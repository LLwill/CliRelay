package cliproxy

import coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"

type runtimeAuthUpdateAction string

const (
	runtimeAuthUpdateActionAdd    runtimeAuthUpdateAction = "add"
	runtimeAuthUpdateActionModify runtimeAuthUpdateAction = "modify"
	runtimeAuthUpdateActionDelete runtimeAuthUpdateAction = "delete"
)

type runtimeAuthUpdate struct {
	Action runtimeAuthUpdateAction
	ID     string
	Auth   *coreauth.Auth
}
