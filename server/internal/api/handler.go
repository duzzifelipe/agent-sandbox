package api

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/duck-labs/agentsdx-server/internal/profile"
	"github.com/duck-labs/agentsdx-server/internal/session"
	"github.com/duck-labs/agentsdx-server/internal/vault"
	"github.com/duck-labs/agentsdx-server/internal/vm"
	"github.com/duck-labs/agentsdx-shared/types"
)

// ImageBuilder is the interface for building VM images.
type ImageBuilder interface {
	Build(ctx context.Context, profile types.ProfileSpec) (string, error)
}

// Handler holds all dependencies for the HTTP API.
type Handler struct {
	profiles    *profile.Store
	sessions    *session.Manager
	images      *vm.ImageStore
	builder     ImageBuilder
	vaultDir    string
	vaultSecret string
}

// NewHandler creates a Handler with the given dependencies.
func NewHandler(
	profiles *profile.Store,
	sessions *session.Manager,
	images *vm.ImageStore,
	builder ImageBuilder,
	vaultDir string,
	vaultSecret string,
) *Handler {
	return &Handler{
		profiles:    profiles,
		sessions:    sessions,
		images:      images,
		builder:     builder,
		vaultDir:    vaultDir,
		vaultSecret: vaultSecret,
	}
}

// Router builds and returns the chi router with all routes registered.
func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/profiles", h.listProfiles)
	r.Post("/profiles", h.createProfile)
	r.Get("/profiles/{name}", h.getProfile)
	r.Put("/profiles/{name}", h.updateProfile)
	r.Delete("/profiles/{name}", h.deleteProfile)
	r.Post("/profiles/{name}/credentials", h.setCredentials)
	r.Put("/profiles/{name}/secrets/{key}", h.setSecret)
	r.Delete("/profiles/{name}/secrets/{key}", h.deleteSecret)
	r.Get("/profiles/{name}/secrets", h.listSecrets)

	r.Post("/sessions", h.createSession)
	r.Get("/sessions/{id}", h.getSession)
	r.Get("/sessions/{id}/key", h.getSessionKey)
	r.Get("/sessions/{id}/agent-state", h.getAgentState)
	r.Post("/sessions/{id}/stop", h.stopSession)
	r.Post("/sessions/{id}/vault-sync", h.vaultSync)
	r.Post("/images/build", h.buildImage)
	r.Get("/images", h.listImages)

	return r
}

func (h *Handler) listProfiles(w http.ResponseWriter, r *http.Request) {
	specs, err := h.profiles.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, specs)
}

func (h *Handler) createProfile(w http.ResponseWriter, r *http.Request) {
	var spec types.ProfileSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.profiles.Create(spec); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, spec)
}

func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	spec, err := h.profiles.Get(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, spec)
}

func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var spec types.ProfileSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	spec.Name = name
	if err := h.profiles.Delete(name); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := h.profiles.Create(spec); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, spec)
}

func (h *Handler) deleteProfile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.profiles.Delete(name); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setCredentials(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	tarball, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body")
		return
	}

	// Bootstrap vault with SSH keys on first call; leave existing keys untouched on subsequent calls.
	if !vault.VaultExists(h.vaultDir, name) {
		vmPriv, vmPub, err := vault.GenerateKeyPair()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate vm key pair")
			return
		}
		gitPriv, gitPub, err := vault.GenerateKeyPair()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate git key pair")
			return
		}
		vd := types.DefaultVaultData()
		vd.VMAccessPrivateKey = vmPriv
		vd.VMAccessPublicKey = vmPub
		vd.GitPrivateKey = gitPriv
		vd.GitPublicKey = gitPub
		if err := vault.StoreVaultData(h.vaultDir, name, h.vaultSecret, vd); err != nil {
			writeError(w, http.StatusInternalServerError, "store vault")
			return
		}
	}

	agentStatePath := filepath.Join(h.vaultDir, name+"-agent-state.tar")
	if err := os.WriteFile(agentStatePath, tarball, 0600); err != nil {
		writeError(w, http.StatusInternalServerError, "store agent state")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	var req types.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	spec, err := h.profiles.Get(req.ProfileName)
	if err != nil {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	id, err := h.sessions.Start(r.Context(), spec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp, _ := h.sessions.Get(id)
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	resp, err := h.sessions.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) getSessionKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	resp, err := h.sessions.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	vaultData, err := vault.LoadVaultData(h.vaultDir, resp.Profile, h.vaultSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load vault")
		return
	}
	writeJSON(w, http.StatusOK, types.VMKeyResponse{PrivateKey: vaultData.VMAccessPrivateKey})
}

func (h *Handler) stopSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.sessions.Stop(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) vaultSync(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	resp, err := h.sessions.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	tarball, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body")
		return
	}

	key, err := vault.DeriveKey(h.vaultSecret, resp.Profile+"-agent-state")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "derive key")
		return
	}
	encrypted, err := vault.Encrypt(key, tarball)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encrypt")
		return
	}
	path := filepath.Join(h.vaultDir, resp.Profile+"-agent-state.tar.enc")
	if err := os.WriteFile(path, encrypted, 0600); err != nil {
		writeError(w, http.StatusInternalServerError, "store agent state")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) buildImage(w http.ResponseWriter, r *http.Request) {
	var req types.BuildImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	spec, err := h.profiles.Get(req.ProfileName)
	if err != nil {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	go func() {
		if _, err := h.builder.Build(context.Background(), spec); err != nil {
			log.Printf("buildImage: profile %s: %v", req.ProfileName, err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "building", "profile": req.ProfileName})
}

func (h *Handler) listImages(w http.ResponseWriter, r *http.Request) {
	entries, err := h.images.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (h *Handler) getAgentState(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	resp, err := h.sessions.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	// Try encrypted agent state first (from vault-sync).
	encPath := filepath.Join(h.vaultDir, resp.Profile+"-agent-state.tar.enc")
	if encData, err := os.ReadFile(encPath); err == nil {
		key, err := vault.DeriveKey(h.vaultSecret, resp.Profile+"-agent-state")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "derive key")
			return
		}
		plaintext, err := vault.Decrypt(key, encData)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "decrypt agent state")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(plaintext)
		return
	}

	// Fall back to plain agent state tarball (from setCredentials).
	rawPath := filepath.Join(h.vaultDir, resp.Profile+"-agent-state.tar")
	if rawData, err := os.ReadFile(rawPath); err == nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(rawData)
		return
	}

	// No agent state yet.
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setSecret(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	key := chi.URLParam(r, "key")

	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	var vd types.VaultData
	if vault.VaultExists(h.vaultDir, name) {
		loaded, err := vault.LoadVaultData(h.vaultDir, name, h.vaultSecret)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load vault")
			return
		}
		vd = loaded
	} else {
		vmPriv, vmPub, err := vault.GenerateKeyPair()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate vm key pair")
			return
		}
		gitPriv, gitPub, err := vault.GenerateKeyPair()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate git key pair")
			return
		}
		vd = types.DefaultVaultData()
		vd.VMAccessPrivateKey = vmPriv
		vd.VMAccessPublicKey = vmPub
		vd.GitPrivateKey = gitPriv
		vd.GitPublicKey = gitPub
	}

	if vd.Secrets == nil {
		vd.Secrets = make(map[string]string)
	}
	vd.Secrets[key] = req.Value

	if err := vault.StoreVaultData(h.vaultDir, name, h.vaultSecret, vd); err != nil {
		writeError(w, http.StatusInternalServerError, "store vault")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteSecret(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	key := chi.URLParam(r, "key")

	if !vault.VaultExists(h.vaultDir, name) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	vd, err := vault.LoadVaultData(h.vaultDir, name, h.vaultSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load vault")
		return
	}
	delete(vd.Secrets, key)
	if err := vault.StoreVaultData(h.vaultDir, name, h.vaultSecret, vd); err != nil {
		writeError(w, http.StatusInternalServerError, "store vault")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listSecrets(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	if !vault.VaultExists(h.vaultDir, name) {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	vd, err := vault.LoadVaultData(h.vaultDir, name, h.vaultSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load vault")
		return
	}
	keys := make([]string, 0, len(vd.Secrets))
	for k := range vd.Secrets {
		keys = append(keys, k)
	}
	writeJSON(w, http.StatusOK, keys)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	if status >= 500 {
		log.Printf("ERROR %d: %s", status, msg)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}
