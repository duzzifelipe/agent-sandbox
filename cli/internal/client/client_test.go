package client_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/duck-labs/agentsdx-cli/internal/client"
	"github.com/duck-labs/agentsdx-shared/types"
)

func TestListProfiles(t *testing.T) {
	want := []types.ProfileSpec{{Name: "myprofile"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/profiles" || r.Method != http.MethodGet {
			http.Error(w, "unexpected", 400)
			return
		}
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	c := client.New(srv.URL)
	got, err := c.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "myprofile" {
		t.Fatalf("got %+v", got)
	}
}

func TestCreateProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/profiles" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", 400)
			return
		}
		var spec types.ProfileSpec
		json.NewDecoder(r.Body).Decode(&spec)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(spec)
	}))
	defer srv.Close()

	c := client.New(srv.URL)
	spec := types.ProfileSpec{Name: "test"}
	if err := c.CreateProfile(spec); err != nil {
		t.Fatal(err)
	}
}

func TestCreateSession(t *testing.T) {
	want := types.SessionResponse{ID: "sess-1", Profile: "myprofile", State: "pending"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", 400)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	c := client.New(srv.URL)
	got, err := c.CreateSession("myprofile")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "sess-1" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetSession(t *testing.T) {
	want := types.SessionResponse{ID: "sess-1", State: "running", IPAddress: "1.2.3.4"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/sess-1" || r.Method != http.MethodGet {
			http.Error(w, "unexpected", 400)
			return
		}
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	c := client.New(srv.URL)
	got, err := c.GetSession("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.IPAddress != "1.2.3.4" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetSessionKey(t *testing.T) {
	want := types.VMKeyResponse{PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\n..."}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/sess-1/key" || r.Method != http.MethodGet {
			http.Error(w, "unexpected", 400)
			return
		}
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	c := client.New(srv.URL)
	got, err := c.GetSessionKey("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != want.PrivateKey {
		t.Fatalf("got %q", got)
	}
}

func TestStopSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/sess-1/stop" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", 400)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := client.New(srv.URL)
	if err := c.StopSession("sess-1"); err != nil {
		t.Fatal(err)
	}
}

func TestSetCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/profiles/myprofile/credentials" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", 400)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := client.New(srv.URL)
	if err := c.SetCredentials("myprofile", []byte("tardata")); err != nil {
		t.Fatal(err)
	}
}

func TestBuildImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/build" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", 400)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	c := client.New(srv.URL)
	if err := c.BuildImage("myprofile"); err != nil {
		t.Fatal(err)
	}
}
