package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	repo            Repository
	redis           *redis.Client
	jwtSecret       []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
}

func NewService(repo Repository, redis *redis.Client, jwtSecret string, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{
		repo:            repo,
		redis:           redis,
		jwtSecret:       []byte(jwtSecret),
		accessTokenTTL:  accessTTL,
		refreshTokenTTL: refreshTTL,
	}
}

func (s *Service) SignUp(ctx context.Context, email, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = s.repo.CreateUser(ctx, email, string(hash))
	return err
}

func (s *Service) SignIn(ctx context.Context, email, password string) (accessToken, refreshToken string, err error) {
	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return "", "", fmt.Errorf("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", "", fmt.Errorf("invalid credentials")
	}

	accessToken, err = s.generateAccessToken(user.ID, user.TokenVersion)
	if err != nil {
		return "", "", err
	}

	refreshToken, err = s.generateRefreshToken(ctx, user.ID)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func (s *Service) generateAccessToken(userID string, tokenVersion int) (string, error) {
	claims := jwt.MapClaims{
		"sub": userID,
		"ver": tokenVersion,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(s.accessTokenTTL).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
}

func (s *Service) generateRefreshToken(ctx context.Context, userID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	raw := hex.EncodeToString(b)
	hash := sha256.Sum256([]byte(raw))

	if err := s.repo.CreateRefreshToken(ctx, userID, hex.EncodeToString(hash[:]), time.Now().Add(s.refreshTokenTTL)); err != nil {
		return "", err
	}
	return raw, nil
}

func (s *Service) ValidateAccessToken(ctx context.Context, tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return "", fmt.Errorf("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", fmt.Errorf("invalid claims")
	}

	userID, _ := claims["sub"].(string)
	if userID == "" {
		return "", fmt.Errorf("missing subject")
	}

	tokenVer, ok := claims["ver"].(float64)
	if !ok {
		return "", fmt.Errorf("missing version")
	}

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("user not found")
	}
	if user.TokenVersion != int(tokenVer) {
		return "", fmt.Errorf("session revoked")
	}

	blacklisted, err := s.redis.Exists(ctx, "blacklist:"+tokenString).Result()
	if err != nil {
		return "", fmt.Errorf("redis error")
	}
	if blacklisted > 0 {
		return "", fmt.Errorf("token revoked")
	}

	return userID, nil
}

func (s *Service) BlacklistToken(ctx context.Context, tokenString string) error {
	parsed, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return err
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return fmt.Errorf("invalid claims")
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		return fmt.Errorf("missing exp")
	}
	ttl := time.Until(time.Unix(int64(exp), 0))
	if ttl > 0 {
		return s.redis.Set(ctx, "blacklist:"+tokenString, "1", ttl).Err()
	}
	return nil
}

func (s *Service) Refresh(ctx context.Context, rawRefresh string) (string, string, error) {
	hash := sha256.Sum256([]byte(rawRefresh))
	userID, err := s.repo.ValidateAndRotateRefreshToken(ctx, hex.EncodeToString(hash[:]))
	if err != nil {
		return "", "", fmt.Errorf("invalid refresh token")
	}

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return "", "", err
	}

	access, err := s.generateAccessToken(userID, user.TokenVersion)
	if err != nil {
		return "", "", err
	}
	refresh, err := s.generateRefreshToken(ctx, userID)
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

func (s *Service) Logout(ctx context.Context, accessToken string) error {
	return s.BlacklistToken(ctx, accessToken)
}

func (s *Service) LogoutAll(ctx context.Context, userID string) error {
	if err := s.repo.IncrementTokenVersion(ctx, userID); err != nil {
		return err
	}
	return s.repo.RevokeAllRefreshTokens(ctx, userID)
}
