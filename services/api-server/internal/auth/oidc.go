// Package auth はOIDC認証とセッション管理を提供する。
package auth

import (
	"context"
	"fmt"
	"net/url"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// OIDCProvider はOIDC認証フローを管理する。
type OIDCProvider struct {
	provider     *gooidc.Provider
	verifier     *gooidc.IDTokenVerifier
	oauth2Config oauth2.Config
	authCfg      *config.AuthConfig
	roleMapper   directory.GroupRoleMapper
}

// NewOIDCProvider はOIDCプロバイダーを初期化して返す。
func NewOIDCProvider(ctx context.Context, cfg *config.AuthConfig) (*OIDCProvider, error) {
	oidcCfg := cfg.Providers.OIDC
	provider, err := gooidc.NewProvider(ctx, oidcCfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDCプロバイダー初期化失敗 (issuer=%s): %w", oidcCfg.Issuer, err)
	}

	verifier := provider.Verifier(&gooidc.Config{
		ClientID: oidcCfg.ClientID,
	})

	oauth2Cfg := oauth2.Config{
		ClientID:     oidcCfg.ClientID,
		ClientSecret: oidcCfg.ClientSecret,
		RedirectURL:  oidcCfg.RedirectURI,
		Endpoint:     provider.Endpoint(),
		Scopes:       oidcCfg.Scopes,
	}

	return &OIDCProvider{
		provider:     provider,
		verifier:     verifier,
		oauth2Config: oauth2Cfg,
		authCfg:      cfg,
		roleMapper: directory.GroupRoleMapper{
			AdminGroup:    cfg.GroupMappings.Admin,
			OperatorGroup: cfg.GroupMappings.Operator,
			ViewerGroup:   cfg.GroupMappings.Viewer,
		},
	}, nil
}

// AuthCodeURL はOIDC認可コードフローの認証URLを返す。
// external_issuer が設定されている場合、ブラウザ向けに scheme+host を書き換える。
// これにより、内部名前解決（authentik:9000）で discovery しつつ、
// ブラウザには外部URL（localhost:9000）を提示できる。
func (p *OIDCProvider) AuthCodeURL(state, nonce string) string {
	rawURL := p.oauth2Config.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("nonce", nonce),
	)

	extBase := p.authCfg.Providers.OIDC.ExternalIssuer
	if extBase == "" {
		return rawURL
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	ext, err := url.Parse(extBase)
	if err != nil {
		return rawURL
	}
	u.Scheme = ext.Scheme
	u.Host = ext.Host
	return u.String()
}

// idTokenClaims はID tokenから取得するクレームを表す。
type idTokenClaims struct {
	Sub    string   `json:"sub"`
	Email  string   `json:"email"`
	Name   string   `json:"name"`
	Groups []string `json:"groups"`
	Nonce  string   `json:"nonce"`
}

// Exchange は認可コードをトークンと交換しSessionを返す。
// nonceを検証してセキュリティを確保する。
func (p *OIDCProvider) Exchange(ctx context.Context, code, nonce string) (*domain.Session, error) {
	// 認可コードをトークンと交換
	token, err := p.oauth2Config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("トークン交換失敗: %w", err)
	}

	// ID tokenを取得
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("id_token が見つかりません")
	}

	// ID tokenを検証
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("id_token 検証失敗: %w", err)
	}

	// クレームを取得
	var claims idTokenClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("クレーム取得失敗: %w", err)
	}

	// nonce を検証
	if claims.Nonce != nonce {
		return nil, fmt.Errorf("nonce 不一致: expected=%s, got=%s", nonce, claims.Nonce)
	}

	// グループからロールを解決（OIDC/LDAP/SCIM 共通の GroupRoleMapper を利用）
	role := p.roleMapper.Resolve(claims.Groups)

	userClaims := domain.UserClaims{
		Sub:    claims.Sub,
		Email:  claims.Email,
		Name:   claims.Name,
		Groups: claims.Groups,
	}

	refreshToken := ""
	if token.RefreshToken != "" {
		refreshToken = token.RefreshToken
	}

	session := &domain.Session{
		User:         userClaims,
		Role:         role,
		AccessToken:  token.AccessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(p.authCfg.Session.TTLMinutes) * time.Minute),
	}

	return session, nil
}
