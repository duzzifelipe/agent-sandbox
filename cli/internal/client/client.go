package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/duck-labs/agentsdx-shared/types"
)

type Client struct {
	base string
	http *http.Client
}

func New(baseURL string) *Client {
	return &Client{base: baseURL, http: &http.Client{}}
}

func (c *Client) ListProfiles() ([]types.ProfileSpec, error) {
	resp, err := c.http.Get(c.base + "/profiles")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}
	var specs []types.ProfileSpec
	return specs, json.NewDecoder(resp.Body).Decode(&specs)
}

func (c *Client) CreateProfile(spec types.ProfileSpec) error {
	body, _ := json.Marshal(spec)
	resp, err := c.http.Post(c.base+"/profiles", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) CreateSession(profileName string) (types.SessionResponse, error) {
	body, _ := json.Marshal(types.CreateSessionRequest{ProfileName: profileName})
	resp, err := c.http.Post(c.base+"/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		return types.SessionResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return types.SessionResponse{}, fmt.Errorf("server returned %d", resp.StatusCode)
	}
	var s types.SessionResponse
	return s, json.NewDecoder(resp.Body).Decode(&s)
}

func (c *Client) GetSession(id string) (types.SessionResponse, error) {
	resp, err := c.http.Get(c.base + "/sessions/" + id)
	if err != nil {
		return types.SessionResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return types.SessionResponse{}, fmt.Errorf("server returned %d", resp.StatusCode)
	}
	var s types.SessionResponse
	return s, json.NewDecoder(resp.Body).Decode(&s)
}

func (c *Client) GetSessionKey(id string) (string, error) {
	resp, err := c.http.Get(c.base + "/sessions/" + id + "/key")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned %d", resp.StatusCode)
	}
	var k types.VMKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&k); err != nil {
		return "", err
	}
	return k.PrivateKey, nil
}

func (c *Client) StopSession(id string) error {
	resp, err := c.http.Post(c.base+"/sessions/"+id+"/stop", "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) SetCredentials(profileName string, tarball []byte) error {
	resp, err := c.http.Post(c.base+"/profiles/"+profileName+"/credentials", "application/octet-stream", bytes.NewReader(tarball))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) BuildImage(profileName string) error {
	body, _ := json.Marshal(types.BuildImageRequest{ProfileName: profileName})
	resp, err := c.http.Post(c.base+"/images/build", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) SetSecret(profile, key, value string) error {
	body, _ := json.Marshal(map[string]string{"value": value})
	req, err := http.NewRequest(http.MethodPut, c.base+"/profiles/"+profile+"/secrets/"+key, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) DeleteSecret(profile, key string) error {
	req, err := http.NewRequest(http.MethodDelete, c.base+"/profiles/"+profile+"/secrets/"+key, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) ListSecrets(profile string) ([]string, error) {
	resp, err := c.http.Get(c.base + "/profiles/" + profile + "/secrets")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}
	var keys []string
	return keys, json.NewDecoder(resp.Body).Decode(&keys)
}
