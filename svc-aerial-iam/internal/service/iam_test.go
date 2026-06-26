package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-iam/internal/model"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-iam/internal/password"
	"github.com/google/uuid"
)

// fakeRepo is an in-memory Repo for unit tests.
type fakeRepo struct {
	orgs   map[string]*model.Organization // slug → org
	users  map[string]*model.User         // email → user
	usersByID map[string]*model.User
	tokens map[string]*model.RefreshToken // hash → token
	devSeq int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		orgs:      map[string]*model.Organization{},
		users:     map[string]*model.User{},
		usersByID: map[string]*model.User{},
		tokens:    map[string]*model.RefreshToken{},
	}
}

func (f *fakeRepo) CreateOrg(_ context.Context, name, slug string) (*model.Organization, error) {
	o := &model.Organization{ID: uuid.NewString(), Name: name, Slug: slug, Modules: []string{}, CreatedAt: time.Now()}
	f.orgs[slug] = o
	return o, nil
}
func (f *fakeRepo) GetOrgBySlug(_ context.Context, slug string) (*model.Organization, error) {
	if o, ok := f.orgs[slug]; ok {
		return o, nil
	}
	return nil, model.ErrOrgNotFound
}
func (f *fakeRepo) CreateUser(_ context.Context, u *model.User) (*model.User, error) {
	if _, exists := f.users[u.Email]; exists {
		return nil, model.ErrUserExists
	}
	u.ID = uuid.NewString()
	u.IsActive = true
	u.CreatedAt = time.Now()
	u.UpdatedAt = time.Now()
	f.users[u.Email] = u
	f.usersByID[u.ID] = u
	return u, nil
}
func (f *fakeRepo) GetUserByEmail(_ context.Context, email string) (*model.User, error) {
	if u, ok := f.users[email]; ok {
		return u, nil
	}
	return nil, model.ErrUserNotFound
}
func (f *fakeRepo) GetUserByID(_ context.Context, id string) (*model.User, error) {
	if u, ok := f.usersByID[id]; ok {
		return u, nil
	}
	return nil, model.ErrUserNotFound
}
func (f *fakeRepo) UpsertDevice(_ context.Context, userID, fingerprint, name string) (*model.Device, error) {
	f.devSeq++
	return &model.Device{ID: uuid.NewString(), UserID: userID, Fingerprint: fingerprint, DeviceName: name}, nil
}
func (f *fakeRepo) StoreRefreshToken(_ context.Context, t *model.RefreshToken) error {
	t.ID = uuid.NewString()
	t.CreatedAt = time.Now()
	// store a copy
	cp := *t
	f.tokens[t.TokenHash] = &cp
	return nil
}
func (f *fakeRepo) FindRefreshToken(_ context.Context, hash string) (*model.RefreshToken, error) {
	rt, ok := f.tokens[hash]
	if !ok {
		return nil, model.ErrTokenNotFound
	}
	if rt.RevokedAt != nil {
		return rt, model.ErrTokenRevoked
	}
	if time.Now().After(rt.ExpiresAt) {
		return rt, model.ErrTokenExpired
	}
	return rt, nil
}
func (f *fakeRepo) RevokeFamily(_ context.Context, familyID string) error {
	now := time.Now()
	for _, t := range f.tokens {
		if t.FamilyID == familyID && t.RevokedAt == nil {
			t.RevokedAt = &now
		}
	}
	return nil
}
func (f *fakeRepo) RevokeToken(_ context.Context, hash string) error {
	if t, ok := f.tokens[hash]; ok && t.RevokedAt == nil {
		now := time.Now()
		t.RevokedAt = &now
	}
	return nil
}

func newSvc(repo Repo) *IAM {
	iss := jwt.New("test-secret-at-least-16-chars", "aerial-test", "aerial-clients", 15*time.Minute)
	return New(repo, iss, 720*time.Hour)
}

func TestSignupCreatesOrgAndSuperadmin(t *testing.T) {
	svc := newSvc(newFakeRepo())
	u, err := svc.Signup(context.Background(), model.SignupRequest{
		Email: "boss@home.local", Password: "longenoughpw", OrgName: "Home", OrgSlug: "home",
	})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if u.Role != model.RoleSuperadmin {
		t.Fatalf("first user should be superadmin, got %q", u.Role)
	}
	if u.OrgID == "" {
		t.Fatal("user should be attached to an org")
	}
}

func TestSignupRejectsDuplicateEmail(t *testing.T) {
	svc := newSvc(newFakeRepo())
	req := model.SignupRequest{Email: "dup@home.local", Password: "longenoughpw"}
	if _, err := svc.Signup(context.Background(), req); err != nil {
		t.Fatalf("first signup: %v", err)
	}
	if _, err := svc.Signup(context.Background(), req); !errors.Is(err, model.ErrUserExists) {
		t.Fatalf("want ErrUserExists, got %v", err)
	}
}

func TestSignupRejectsBadEmailAndShortPassword(t *testing.T) {
	svc := newSvc(newFakeRepo())
	if _, err := svc.Signup(context.Background(), model.SignupRequest{Email: "not-an-email", Password: "longenoughpw"}); !errors.Is(err, model.ErrInvalidArgument) {
		t.Fatalf("want ErrInvalidArgument for bad email, got %v", err)
	}
	if _, err := svc.Signup(context.Background(), model.SignupRequest{Email: "ok@home.local", Password: "short"}); !errors.Is(err, model.ErrInvalidArgument) {
		t.Fatalf("want ErrInvalidArgument for short password, got %v", err)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	svc := newSvc(newFakeRepo())
	_, _ = svc.Signup(context.Background(), model.SignupRequest{Email: "u@home.local", Password: "correctpassword"})
	if _, err := svc.Login(context.Background(), model.LoginRequest{Email: "u@home.local", Password: "wrongpassword"}); !errors.Is(err, model.ErrBadCredentials) {
		t.Fatalf("want ErrBadCredentials, got %v", err)
	}
}

func TestLoginUnknownUserIsBadCredentials(t *testing.T) {
	svc := newSvc(newFakeRepo())
	if _, err := svc.Login(context.Background(), model.LoginRequest{Email: "ghost@home.local", Password: "whatever12"}); !errors.Is(err, model.ErrBadCredentials) {
		t.Fatalf("unknown user should be ErrBadCredentials (no enumeration), got %v", err)
	}
}

func TestLoginSuccessIssuesVerifiableAccessToken(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo)
	_, _ = svc.Signup(context.Background(), model.SignupRequest{Email: "u@home.local", Password: "correctpassword"})
	tp, err := svc.Login(context.Background(), model.LoginRequest{Email: "u@home.local", Password: "correctpassword", DeviceFingerprint: "dev1"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	iss := jwt.New("test-secret-at-least-16-chars", "aerial-test", "aerial-clients", time.Minute)
	if _, err := iss.Verify(tp.AccessToken); err != nil {
		t.Fatalf("issued access token should verify: %v", err)
	}
	if tp.RefreshToken == "" {
		t.Fatal("expected a refresh token")
	}
}

func TestRefreshRotatesAndDetectsReuse(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo)
	_, _ = svc.Signup(context.Background(), model.SignupRequest{Email: "u@home.local", Password: "correctpassword"})
	tp, _ := svc.Login(context.Background(), model.LoginRequest{Email: "u@home.local", Password: "correctpassword"})

	// First refresh succeeds and returns a new refresh token.
	tp2, err := svc.Refresh(context.Background(), tp.RefreshToken, "")
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if tp2.RefreshToken == tp.RefreshToken {
		t.Fatal("refresh should rotate the token")
	}

	// Replaying the OLD refresh token is reuse → ErrTokenReuse and family revoked.
	if _, err := svc.Refresh(context.Background(), tp.RefreshToken, ""); !errors.Is(err, model.ErrTokenReuse) {
		t.Fatalf("want ErrTokenReuse on replay, got %v", err)
	}

	// After reuse detection the whole family is revoked: even the latest token fails.
	if _, err := svc.Refresh(context.Background(), tp2.RefreshToken, ""); err == nil {
		t.Fatal("after reuse detection the rotated token should also be revoked")
	}
}

func TestLogoutRevokesToken(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo)
	_, _ = svc.Signup(context.Background(), model.SignupRequest{Email: "u@home.local", Password: "correctpassword"})
	tp, _ := svc.Login(context.Background(), model.LoginRequest{Email: "u@home.local", Password: "correctpassword"})

	if err := svc.Logout(context.Background(), tp.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := svc.Refresh(context.Background(), tp.RefreshToken, ""); err == nil {
		t.Fatal("refresh after logout should fail")
	}
}

// Sanity check the password package round-trips (cheap, no DB).
func TestPasswordHashCheck(t *testing.T) {
	h, err := password.Hash("hunter2hunter2")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := password.Check(h, "hunter2hunter2"); err != nil {
		t.Fatalf("check should pass: %v", err)
	}
	if err := password.Check(h, "wrong"); err == nil {
		t.Fatal("check should fail for wrong password")
	}
}
