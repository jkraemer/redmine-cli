package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// registrationServer serves RFC 8414 metadata at metadataPath (with the
// given issuer path appended to the server URL) and an RFC 7591
// registration endpoint at /register. It records the registration request.
type registrationServer struct {
	*httptest.Server
	gotContentType string
	gotRequest     map[string]any
}

func newRegistrationServer(t *testing.T, metadataPath, issuerPath string) *registrationServer {
	t.Helper()
	rs := &registrationServer{}
	mux := http.NewServeMux()
	mux.HandleFunc(metadataPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                rs.URL + issuerPath,
			"registration_endpoint": rs.URL + "/register",
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("registration method=%s want POST", r.Method)
		}
		rs.gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&rs.gotRequest); err != nil {
			t.Errorf("decode registration request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"client_id":     "new-client-id",
			"client_secret": "ignored-secret",
			"scope":         "view_project view_issues",
		})
	})
	rs.Server = httptest.NewServer(mux)
	t.Cleanup(rs.Close)
	return rs
}

func TestRegisterClient_DiscoversAndRegistersPublicClient(t *testing.T) {
	srv := newRegistrationServer(t, "/.well-known/oauth-authorization-server", "")

	reg, err := RegisterClient(srv.URL, "redmine-cli")
	if err != nil {
		t.Fatal(err)
	}
	if reg.ClientID != "new-client-id" {
		t.Errorf("client id=%q want new-client-id", reg.ClientID)
	}
	if !reflect.DeepEqual(reg.Scopes, []string{"view_project", "view_issues"}) {
		t.Errorf("scopes=%v want [view_project view_issues]", reg.Scopes)
	}
	if srv.gotContentType != "application/json" {
		t.Errorf("Content-Type=%q want application/json", srv.gotContentType)
	}
	want := map[string]any{
		"client_name":                "redmine-cli",
		"redirect_uris":              []any{redirectURI},
		"grant_types":                []any{"authorization_code", "refresh_token"},
		"response_types":             []any{"code"},
		"token_endpoint_auth_method": "none",
	}
	if !reflect.DeepEqual(srv.gotRequest, want) {
		t.Errorf("registration request=%v\nwant %v", srv.gotRequest, want)
	}
}

func TestRegisterClient_TrailingSlashInBaseURL(t *testing.T) {
	srv := newRegistrationServer(t, "/.well-known/oauth-authorization-server", "")

	if _, err := RegisterClient(srv.URL+"/", "redmine-cli"); err != nil {
		t.Fatal(err)
	}
}

// RFC 8414 §3.1: for an issuer with a path component, the well-known
// segment goes between the host and the path.
func TestRegisterClient_SubPathInstallUsesPathInsertion(t *testing.T) {
	srv := newRegistrationServer(t, "/.well-known/oauth-authorization-server/redmine", "/redmine")

	reg, err := RegisterClient(srv.URL+"/redmine", "redmine-cli")
	if err != nil {
		t.Fatal(err)
	}
	if reg.ClientID != "new-client-id" {
		t.Errorf("client id=%q", reg.ClientID)
	}
}

func TestRegisterClient_NoMetadataIsUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	_, err := RegisterClient(srv.URL, "redmine-cli")
	if !errors.Is(err, ErrRegistrationUnsupported) {
		t.Fatalf("err=%v want ErrRegistrationUnsupported", err)
	}
}

func TestRegisterClient_NonJSONMetadataIsUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>login</html>"))
	}))
	defer srv.Close()

	_, err := RegisterClient(srv.URL, "redmine-cli")
	if !errors.Is(err, ErrRegistrationUnsupported) {
		t.Fatalf("err=%v want ErrRegistrationUnsupported", err)
	}
}

func TestRegisterClient_MetadataWithoutRegistrationEndpointIsUnsupported(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"issuer": srv.URL})
	}))
	defer srv.Close()

	_, err := RegisterClient(srv.URL, "redmine-cli")
	if !errors.Is(err, ErrRegistrationUnsupported) {
		t.Fatalf("err=%v want ErrRegistrationUnsupported", err)
	}
}

// RFC 8414 §3.3: the issuer in the metadata must be identical to the
// issuer the metadata was requested for, or it must not be used.
func TestRegisterClient_IssuerMismatchIsRejected(t *testing.T) {
	srv := newRegistrationServer(t, "/.well-known/oauth-authorization-server", "/elsewhere")

	_, err := RegisterClient(srv.URL, "redmine-cli")
	if err == nil || !strings.Contains(err.Error(), "issuer") {
		t.Fatalf("err=%v want issuer mismatch error", err)
	}
	if srv.gotRequest != nil {
		t.Error("registration must not be attempted after an issuer mismatch")
	}
}

func TestRegisterClient_RegistrationErrorIsReported(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid_client_metadata","error_description":"redirect_uris \u001b[31mbad"}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                srv.URL,
			"registration_endpoint": srv.URL + "/register",
		})
	}))
	defer srv.Close()

	_, err := RegisterClient(srv.URL, "redmine-cli")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid_client_metadata") || !strings.Contains(err.Error(), "redirect_uris") {
		t.Errorf("err=%v want server error code and description", err)
	}
	if strings.Contains(err.Error(), "\x1b") {
		t.Errorf("err=%q contains terminal control bytes", err.Error())
	}
}

func TestRegisterClient_NoScopeInResponseYieldsNoScopes(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"client_id":"cid"}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                srv.URL,
			"registration_endpoint": srv.URL + "/register",
		})
	}))
	defer srv.Close()

	reg, err := RegisterClient(srv.URL, "redmine-cli")
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Scopes) != 0 {
		t.Errorf("scopes=%v want none", reg.Scopes)
	}
}

func TestRegisterClient_MissingClientIDIsError(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                srv.URL,
			"registration_endpoint": srv.URL + "/register",
		})
	}))
	defer srv.Close()

	_, err := RegisterClient(srv.URL, "redmine-cli")
	if err == nil || !strings.Contains(err.Error(), "client_id") {
		t.Fatalf("err=%v want missing client_id error", err)
	}
}
