package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const geetestValidateEndpoint = "https://gcaptcha4.geetest.com/validate"

type geetestValidator interface {
	validate(req geetestValidateRequest) (bool, error)
}

var newGeetestValidator = func(captchaID, captchaKey string) geetestValidator {
	return newGeetestService(captchaID, captchaKey)
}

type geetestService struct {
	captchaID   string
	captchaKey  string
	validateURL string
	client      *http.Client
}

type geetestValidateRequest struct {
	LotNumber     string `json:"lot_number"`
	CaptchaOutput string `json:"captcha_output"`
	PassToken     string `json:"pass_token"`
	GenTime       string `json:"gen_time"`
}

type geetestValidateResponse struct {
	Result string `json:"result"`
	Reason string `json:"reason"`
}

func newGeetestService(captchaID, captchaKey string) *geetestService {
	return &geetestService{
		captchaID:   captchaID,
		captchaKey:  captchaKey,
		validateURL: geetestValidateEndpoint,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (g *geetestService) validate(req geetestValidateRequest) (bool, error) {
	if g.captchaID == "" || g.captchaKey == "" {
		return false, fmt.Errorf("geetest not configured")
	}

	signToken := g.hmacSha256(g.captchaKey, req.LotNumber)
	form := url.Values{}
	form.Set("lot_number", req.LotNumber)
	form.Set("captcha_output", req.CaptchaOutput)
	form.Set("pass_token", req.PassToken)
	form.Set("gen_time", req.GenTime)
	form.Set("sign_token", signToken)

	apiURL := fmt.Sprintf("%s?captcha_id=%s", g.validateURL, url.QueryEscape(g.captchaID))
	httpReq, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return false, fmt.Errorf("build geetest request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return false, fmt.Errorf("geetest api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, fmt.Errorf("geetest api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result geetestValidateResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return false, fmt.Errorf("parse geetest response: %w", err)
	}

	return result.Result == "success", nil
}

func (g *geetestService) hmacSha256(key, data string) string {
	h := hmac.New(sha256.New, []byte(key))
	_, _ = h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}
