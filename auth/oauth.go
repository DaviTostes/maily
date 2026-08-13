package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gmailapi "google.golang.org/api/gmail/v1"
)

const (
	appDirName     = "maily"
	tokenFile      = "token.json"
	credsFile      = "credentials.json"
	callbackPath   = "/oauth/callback"
	defaultTimeout = 2 * time.Minute
)

// ConfigDir returns ~/.config/maily, creating it if needed.
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func credsPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, credsFile), nil
}

func tokenPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, tokenFile), nil
}

// LoadConfig reads credentials.json and returns an oauth2.Config with the
// Gmail modify scope.
func LoadConfig() (*oauth2.Config, error) {
	p, err := credsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf(
				"credentials file missing at %s\n\n"+
					"To fix:\n"+
					"  1. Create an OAuth client ID in Google Cloud Console (type: Desktop app or Web app).\n"+
					"  2. Enable the Gmail API for the project.\n"+
					"  3. Download the client secret JSON and save it to the path above.\n", p)
		}
		return nil, err
	}
	cfg, err := google.ConfigFromJSON(data, gmailapi.GmailModifyScope)
	if err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	return cfg, nil
}

func loadToken() (*oauth2.Token, error) {
	p, err := tokenPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	if err := json.NewDecoder(f).Decode(tok); err != nil {
		return nil, err
	}
	return tok, nil
}

func saveToken(tok *oauth2.Token) error {
	p, err := tokenPath()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(tok)
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	_ = cmd.Start()
}

// interactiveFlow runs a local HTTP listener to capture the OAuth redirect.
func interactiveFlow(ctx context.Context, cfg *oauth2.Config) (*oauth2.Token, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://127.0.0.1:%d%s", port, callbackPath)

	cfgCopy := *cfg
	cfgCopy.RedirectURL = redirect

	state := fmt.Sprintf("state-%d", time.Now().UnixNano())
	authURL := cfgCopy.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	type result struct {
		code string
		err  error
	}
	ch := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errStr := q.Get("error"); errStr != "" {
			http.Error(w, "oauth error: "+errStr, http.StatusBadRequest)
			ch <- result{err: fmt.Errorf("oauth error: %s", errStr)}
			return
		}
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			ch <- result{err: fmt.Errorf("state mismatch")}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			ch <- result{err: fmt.Errorf("missing code")}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body style="font-family:system-ui;background:#000000;color:#E6E6E6;padding:3rem;text-align:center"><h1>maily authorized</h1><p>You can close this tab and return to the terminal.</p></body></html>`))
		ch <- result{code: code}
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	defer srv.Close()

	fmt.Fprintln(os.Stderr, "Opening browser for Gmail authorization…")
	fmt.Fprintln(os.Stderr, "If it does not open, visit:")
	fmt.Fprintln(os.Stderr, "  "+authURL)
	openBrowser(authURL)

	waitCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	var res result
	select {
	case res = <-ch:
	case <-waitCtx.Done():
		return nil, fmt.Errorf("timed out waiting for authorization")
	}
	if res.err != nil {
		return nil, res.err
	}

	tok, err := cfgCopy.Exchange(ctx, res.code)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	return tok, nil
}

// Client returns an authorized *http.Client for the Gmail API, running the
// interactive flow if no cached token exists.
func Client(ctx context.Context) (*http.Client, *oauth2.Config, *oauth2.Token, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, nil, nil, err
	}
	tok, err := loadToken()
	if err != nil {
		tok, err = interactiveFlow(ctx, cfg)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := saveToken(tok); err != nil {
			return nil, nil, nil, fmt.Errorf("save token: %w", err)
		}
	}

	src := cfg.TokenSource(ctx, tok)
	refreshed, err := src.Token()
	if err != nil {
		// Token likely revoked or refresh missing; re-auth.
		tok, err = interactiveFlow(ctx, cfg)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := saveToken(tok); err != nil {
			return nil, nil, nil, fmt.Errorf("save token: %w", err)
		}
		src = cfg.TokenSource(ctx, tok)
		refreshed, _ = src.Token()
	}
	if refreshed != nil && refreshed.AccessToken != tok.AccessToken {
		_ = saveToken(refreshed)
		tok = refreshed
	}

	return oauth2.NewClient(ctx, src), cfg, tok, nil
}

// HasCredentials reports whether credentials.json exists.
func HasCredentials() bool {
	p, err := credsPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// CredentialsPath returns the expected credentials.json location for error messages.
func CredentialsPath() string {
	p, err := credsPath()
	if err != nil {
		return filepath.Join("~", ".config", appDirName, credsFile)
	}
	return p
}

// sanity compile check for url import
var _ = url.QueryEscape
var _ = strings.TrimSpace
