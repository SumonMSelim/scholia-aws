package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cip "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
)

type fakeIDP struct {
	start      *cip.InitiateAuthOutput
	startErr   error
	next       *cip.RespondToAuthChallengeOutput
	nextErr    error
	confirm    *cip.RespondToAuthChallengeOutput
	confirmErr error
	calls      []types.ChallengeNameType

	signUp        *cip.SignUpOutput
	signUpErr     error
	signUpIn      *cip.SignUpInput
	confirmed     *cip.ConfirmSignUpOutput
	confirmedErr  error
	confirmedIn   *cip.ConfirmSignUpInput
	resendErr     error
	resends       int
	sessionAuth   *cip.InitiateAuthOutput
	sessionErr    error
	sessionAuthIn *cip.InitiateAuthInput
}

func (f *fakeIDP) InitiateAuth(_ context.Context, in *cip.InitiateAuthInput, _ ...func(*cip.Options)) (*cip.InitiateAuthOutput, error) {
	if in.Session != nil {
		f.sessionAuthIn = in
		return f.sessionAuth, f.sessionErr
	}
	return f.start, f.startErr
}

func (f *fakeIDP) SignUp(_ context.Context, in *cip.SignUpInput, _ ...func(*cip.Options)) (*cip.SignUpOutput, error) {
	f.signUpIn = in
	return f.signUp, f.signUpErr
}

func (f *fakeIDP) ConfirmSignUp(_ context.Context, in *cip.ConfirmSignUpInput, _ ...func(*cip.Options)) (*cip.ConfirmSignUpOutput, error) {
	f.confirmedIn = in
	return f.confirmed, f.confirmedErr
}

func (f *fakeIDP) ResendConfirmationCode(context.Context, *cip.ResendConfirmationCodeInput, ...func(*cip.Options)) (*cip.ResendConfirmationCodeOutput, error) {
	f.resends++
	return &cip.ResendConfirmationCodeOutput{}, f.resendErr
}

func (f *fakeIDP) RespondToAuthChallenge(_ context.Context, in *cip.RespondToAuthChallengeInput, _ ...func(*cip.Options)) (*cip.RespondToAuthChallengeOutput, error) {
	f.calls = append(f.calls, in.ChallengeName)
	if in.ChallengeName == types.ChallengeNameTypeSelectChallenge {
		return f.next, f.nextErr
	}
	return f.confirm, f.confirmErr
}

func idToken(sub string) string {
	payload, _ := json.Marshal(map[string]string{"sub": sub})
	return "h." + base64.RawURLEncoding.EncodeToString(payload) + ".s"
}

func TestCognitoEmailOTP(t *testing.T) {
	idp := &fakeIDP{
		start: &cip.InitiateAuthOutput{
			ChallengeName: types.ChallengeNameTypeEmailOtp,
			Session:       aws.String("s1"),
		},
		confirm: &cip.RespondToAuthChallengeOutput{
			AuthenticationResult: &types.AuthenticationResultType{IdToken: aws.String(idToken("sub-1"))},
		},
	}
	c := &Cognito{api: idp, clientID: "client"}
	session, err := c.Start(context.Background(), "ada@example.com")
	if err != nil || session != "s1" {
		t.Fatalf("start: %q %v", session, err)
	}
	id, err := c.Confirm(context.Background(), "ada@example.com", session, "123456")
	if err != nil || id.Subject != "sub-1" || id.Email != "ada@example.com" {
		t.Fatalf("confirm: %+v %v", id, err)
	}
}

func TestCognitoSelectsEmailOTP(t *testing.T) {
	idp := &fakeIDP{
		start: &cip.InitiateAuthOutput{
			ChallengeName: types.ChallengeNameTypeSelectChallenge,
			Session:       aws.String("pick"),
		},
		next: &cip.RespondToAuthChallengeOutput{
			ChallengeName: types.ChallengeNameTypeEmailOtp,
			Session:       aws.String("s2"),
		},
	}
	c := &Cognito{api: idp, clientID: "client"}
	session, err := c.Start(context.Background(), "ada@example.com")
	if err != nil || session != "s2" {
		t.Fatalf("start: %q %v", session, err)
	}
	if len(idp.calls) != 1 || idp.calls[0] != types.ChallengeNameTypeSelectChallenge {
		t.Fatalf("calls = %v", idp.calls)
	}
}

func TestCognitoRejects(t *testing.T) {
	c := &Cognito{api: &fakeIDP{}, clientID: "client"}
	tests := []struct {
		name string
		run  func() error
	}{
		{"bad email", func() error {
			_, err := c.Start(context.Background(), "not-an-email")
			return err
		}},
		{"initiate fails", func() error {
			c.api = &fakeIDP{startErr: errors.New("no such user ada@example.com")}
			_, err := c.Start(context.Background(), "ada@example.com")
			return err
		}},
		{"empty session", func() error {
			c.api = &fakeIDP{start: &cip.InitiateAuthOutput{ChallengeName: types.ChallengeNameTypeEmailOtp}}
			_, err := c.Start(context.Background(), "ada@example.com")
			return err
		}},
		{"unexpected challenge", func() error {
			c.api = &fakeIDP{start: &cip.InitiateAuthOutput{ChallengeName: types.ChallengeNameTypePassword, Session: aws.String("s")}}
			_, err := c.Start(context.Background(), "ada@example.com")
			return err
		}},
		{"select fails", func() error {
			c.api = &fakeIDP{
				start:   &cip.InitiateAuthOutput{ChallengeName: types.ChallengeNameTypeSelectChallenge, Session: aws.String("s")},
				nextErr: errors.New("denied"),
			}
			_, err := c.Start(context.Background(), "ada@example.com")
			return err
		}},
		{"bad code", func() error {
			c.api = &fakeIDP{confirmErr: errors.New("wrong code for ada@example.com")}
			_, err := c.Confirm(context.Background(), "ada@example.com", "s", "000")
			return err
		}},
		{"token without subject", func() error {
			c.api = &fakeIDP{confirm: &cip.RespondToAuthChallengeOutput{
				AuthenticationResult: &types.AuthenticationResultType{IdToken: aws.String("h.e30.s")},
			}}
			_, err := c.Confirm(context.Background(), "ada@example.com", "s", "123456")
			return err
		}},
		{"missing code", func() error {
			_, err := c.Confirm(context.Background(), "ada@example.com", "s", " ")
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.Contains(err.Error(), "@") {
				t.Fatalf("error leaked an address: %v", err)
			}
		})
	}
}

func TestCognitoSignsUpANewAddress(t *testing.T) {
	idp := &fakeIDP{
		startErr:    &types.UserNotFoundException{Message: aws.String("User does not exist.")},
		signUp:      &cip.SignUpOutput{Session: aws.String("up-1")},
		confirmed:   &cip.ConfirmSignUpOutput{Session: aws.String("in-1")},
		sessionAuth: &cip.InitiateAuthOutput{AuthenticationResult: &types.AuthenticationResultType{IdToken: aws.String(idToken("sub-new"))}},
	}
	c := &Cognito{api: idp, clientID: "client"}
	session, err := c.Start(context.Background(), "new@example.com")
	if err != nil || session != "signup:up-1" {
		t.Fatalf("start: %q %v", session, err)
	}
	in := idp.signUpIn
	if in == nil || aws.ToString(in.Username) != "new@example.com" || in.Password != nil ||
		len(in.UserAttributes) != 1 || aws.ToString(in.UserAttributes[0].Name) != "email" {
		t.Fatalf("sign-up input = %+v", in)
	}
	id, err := c.Confirm(context.Background(), "new@example.com", session, "654321")
	if err != nil || id.Subject != "sub-new" {
		t.Fatalf("confirm: %+v %v", id, err)
	}
	if aws.ToString(idp.confirmedIn.ConfirmationCode) != "654321" || aws.ToString(idp.confirmedIn.Session) != "up-1" {
		t.Fatalf("confirm sign-up input = %+v", idp.confirmedIn)
	}
	if aws.ToString(idp.sessionAuthIn.Session) != "in-1" || idp.sessionAuthIn.AuthFlow != types.AuthFlowTypeUserAuth {
		t.Fatalf("session sign-in input = %+v", idp.sessionAuthIn)
	}
}

func TestCognitoFinishesAnUnconfirmedSignUp(t *testing.T) {
	idp := &fakeIDP{
		startErr:    &types.UserNotConfirmedException{Message: aws.String("not confirmed")},
		confirmed:   &cip.ConfirmSignUpOutput{Session: aws.String("in-2")},
		sessionAuth: &cip.InitiateAuthOutput{AuthenticationResult: &types.AuthenticationResultType{IdToken: aws.String(idToken("sub-2"))}},
	}
	c := &Cognito{api: idp, clientID: "client"}
	session, err := c.Start(context.Background(), "late@example.com")
	if err != nil || session != "signup:" || idp.resends != 1 {
		t.Fatalf("start: %q %v resends %d", session, err, idp.resends)
	}
	if id, err := c.Confirm(context.Background(), "late@example.com", session, "111111"); err != nil || id.Subject != "sub-2" {
		t.Fatalf("confirm: %+v %v", id, err)
	}
	if idp.confirmedIn.Session != nil {
		t.Fatalf("a resent code has no sign-up session: %+v", idp.confirmedIn)
	}
}

func TestCognitoSignUpFailures(t *testing.T) {
	notFound := &types.UserNotFoundException{Message: aws.String("no user new@example.com")}
	tests := []struct {
		name string
		idp  *fakeIDP
		run  func(*Cognito) error
	}{
		{"sign-up fails", &fakeIDP{startErr: notFound, signUpErr: errors.New("bad address new@example.com")}, func(c *Cognito) error {
			_, err := c.Start(context.Background(), "new@example.com")
			return err
		}},
		{"resend fails", &fakeIDP{startErr: &types.UserNotConfirmedException{}, resendErr: errors.New("limit")}, func(c *Cognito) error {
			_, err := c.Start(context.Background(), "new@example.com")
			return err
		}},
		{"wrong code", &fakeIDP{confirmedErr: errors.New("CodeMismatch for new@example.com")}, func(c *Cognito) error {
			_, err := c.Confirm(context.Background(), "new@example.com", "signup:up", "000000")
			return err
		}},
		{"no session to sign in", &fakeIDP{confirmed: &cip.ConfirmSignUpOutput{}}, func(c *Cognito) error {
			_, err := c.Confirm(context.Background(), "new@example.com", "signup:up", "123456")
			return err
		}},
		{"session sign-in fails", &fakeIDP{confirmed: &cip.ConfirmSignUpOutput{Session: aws.String("in")}, sessionErr: errors.New("expired")}, func(c *Cognito) error {
			_, err := c.Confirm(context.Background(), "new@example.com", "signup:up", "123456")
			return err
		}},
		{"token without subject", &fakeIDP{
			confirmed:   &cip.ConfirmSignUpOutput{Session: aws.String("in")},
			sessionAuth: &cip.InitiateAuthOutput{AuthenticationResult: &types.AuthenticationResultType{IdToken: aws.String("h.e30.s")}},
		}, func(c *Cognito) error {
			_, err := c.Confirm(context.Background(), "new@example.com", "signup:up", "123456")
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(&Cognito{api: tt.idp, clientID: "client"})
			if err == nil || strings.Contains(err.Error(), "@") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
