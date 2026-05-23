package types

const (
	SessionStatePending   = "pending"
	SessionStateStarting  = "starting"
	SessionStateRunning   = "running"
	SessionStateStopping  = "stopping"
	SessionStateDestroyed = "destroyed"
)

type CreateSessionRequest struct {
	ProfileName string `json:"profile_name"`
}

type SessionResponse struct {
	ID        string `json:"id"`
	Profile   string `json:"profile"`
	State     string `json:"state"`
	IPAddress string `json:"ip_address,omitempty"`
}

type VMKeyResponse struct {
	PrivateKey string `json:"private_key"`
}

type BuildImageRequest struct {
	ProfileName string `json:"profile_name"`
}

type ImageEntry struct {
	ProfileName string `json:"profile_name"`
	Hetzner     string `json:"hetzner,omitempty"`
}
