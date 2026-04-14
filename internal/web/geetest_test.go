package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestGeetestValidateBuildsSignedRequest(t *testing.T) {
	var method string
	var contentType string
	var query url.Values
	var form url.Values

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		contentType = r.Header.Get("Content-Type")
		query = r.URL.Query()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		form, err = url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer server.Close()

	svc := newGeetestService("captcha-id", "captcha-key")
	svc.validateURL = server.URL
	svc.client = server.Client()

	valid, err := svc.validate(geetestValidateRequest{
		LotNumber:     "lot-123",
		CaptchaOutput: "output-456",
		PassToken:     "token-789",
		GenTime:       "111222333",
	})
	if err != nil {
		t.Fatalf("validate returned error: %v", err)
	}
	if !valid {
		t.Fatal("expected validate to succeed")
	}
	if method != http.MethodPost {
		t.Fatalf("unexpected method: got %s want %s", method, http.MethodPost)
	}
	if contentType != "application/x-www-form-urlencoded" {
		t.Fatalf("unexpected content type: %s", contentType)
	}
	if query.Get("captcha_id") != "captcha-id" {
		t.Fatalf("unexpected captcha id query: %v", query)
	}
	if form.Get("lot_number") != "lot-123" || form.Get("captcha_output") != "output-456" || form.Get("pass_token") != "token-789" || form.Get("gen_time") != "111222333" {
		t.Fatalf("unexpected form payload: %v", form)
	}
	if form.Get("sign_token") != svc.hmacSha256("captcha-key", "lot-123") {
		t.Fatalf("unexpected sign token: %v", form)
	}
}

func TestGeetestValidateRejectsNon200Responses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream failed", http.StatusBadGateway)
	}))
	defer server.Close()

	svc := newGeetestService("captcha-id", "captcha-key")
	svc.validateURL = server.URL
	svc.client = server.Client()

	valid, err := svc.validate(geetestValidateRequest{LotNumber: "lot-123"})
	if err == nil {
		t.Fatal("expected validate to fail")
	}
	if valid {
		t.Fatal("expected validate to report false on upstream error")
	}
}
