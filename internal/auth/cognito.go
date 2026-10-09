package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	cip "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
)

// Cognito runs the email one-time-code flow against a real user pool.
// The client id is enough. The pool id is not sent on these calls.
type Cognito struct {
	api      idpAPI
	clientID string
}

type idpAPI interface {
	InitiateAuth(context.Context, *cip.InitiateAuthInput, ...func(*cip.Options)) (*cip.InitiateAuthOutput, error)
	RespondToAuthChallenge(context.Context, *cip.RespondToAuthChallengeInput, ...func(*cip.Options)) (*cip.RespondToAuthChallengeOutput, error)
	SignUp(context.Context, *cip.SignUpInput, ...func(*cip.Options)) (*cip.SignUpOutput, error)
	ConfirmSignUp(context.Context, *cip.ConfirmSignUpInput, ...func(*cip.Options)) (*cip.ConfirmSignUpOutput, error)
	ResendConfirmationCode(context.Context, *cip.ResendConfirmationCodeInput, ...func(*cip.Options)) (*cip.ResendConfirmationCodeOutput, error)
}

// signUpPrefix marks a session handle from a first sign-up. Confirm then checks
// the code with ConfirmSignUp, whose session signs the new user straight in.
const signUpPrefix = "signup:"

// OpenCognito builds a client that always calls Amazon Cognito.
// A process-wide AWS_ENDPOINT_URL points DynamoDB and S3 at floci.
// Floci does not emulate these calls, so this client ignores that endpoint.
func OpenCognito(cfg aws.Config, clientID string) *Cognito {
	endpoint := "https://cognito-idp." + cfg.Region + ".amazonaws.com"
	return &Cognito{
		api: cip.NewFromConfig(cfg, func(o *cip.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		}),
		clientID: clientID,
	}
}

// Start asks Cognito for an email code. The returned session is required to confirm it.
// A new address is signed up without a password: Cognito then emails a
// confirmation code, which Confirm accepts the same way as a sign-in code.
// Callers must not log the error: Cognito messages can contain the email address.
func (c *Cognito) Start(ctx context.Context, email string) (string, error) {
	email = strings.TrimSpace(email)
	if !validEmail(email) {
		return "", errors.New("could not send a code")
	}
	out, err := c.api.InitiateAuth(ctx, &cip.InitiateAuthInput{
		AuthFlow: types.AuthFlowTypeUserAuth,
		ClientId: aws.String(c.clientID),
		AuthParameters: map[string]string{
			"USERNAME":            email,
			"PREFERRED_CHALLENGE": "EMAIL_OTP",
		},
	})
	var notFound *types.UserNotFoundException
	var notConfirmed *types.UserNotConfirmedException
	switch {
	case errors.As(err, &notFound):
		return c.signUp(ctx, email)
	case errors.As(err, &notConfirmed):
		// An earlier sign-up was never confirmed. A fresh code finishes it.
		if _, err := c.api.ResendConfirmationCode(ctx, &cip.ResendConfirmationCodeInput{
			ClientId: aws.String(c.clientID), Username: aws.String(email),
		}); err != nil {
			return "", errors.New("could not send a code")
		}
		return signUpPrefix, nil
	case err != nil:
		return "", errors.New("could not send a code")
	}
	return c.emailSession(ctx, email, out.ChallengeName, out.Session)
}

// signUp creates a passwordless user. The pool emails the confirmation code.
func (c *Cognito) signUp(ctx context.Context, email string) (string, error) {
	out, err := c.api.SignUp(ctx, &cip.SignUpInput{
		ClientId:       aws.String(c.clientID),
		Username:       aws.String(email),
		UserAttributes: []types.AttributeType{{Name: aws.String("email"), Value: aws.String(email)}},
	})
	if err != nil {
		return "", errors.New("could not send a code")
	}
	return signUpPrefix + aws.ToString(out.Session), nil
}

// confirmSignUp checks a sign-up code, then signs in with the session it returns.
func (c *Cognito) confirmSignUp(ctx context.Context, email, session, code string) (Identity, error) {
	in := &cip.ConfirmSignUpInput{
		ClientId: aws.String(c.clientID), Username: aws.String(email), ConfirmationCode: aws.String(code),
	}
	if session != "" {
		in.Session = aws.String(session)
	}
	confirmed, err := c.api.ConfirmSignUp(ctx, in)
	if err != nil || confirmed.Session == nil || *confirmed.Session == "" {
		// Without a session the account is confirmed but the next sign-in needs a new code.
		return Identity{}, errors.New("code was not accepted")
	}
	out, err := c.api.InitiateAuth(ctx, &cip.InitiateAuthInput{
		AuthFlow:       types.AuthFlowTypeUserAuth,
		ClientId:       aws.String(c.clientID),
		Session:        confirmed.Session,
		AuthParameters: map[string]string{"USERNAME": email},
	})
	if err != nil || out.AuthenticationResult == nil || out.AuthenticationResult.IdToken == nil {
		return Identity{}, errors.New("code was not accepted")
	}
	subject, err := subjectFromIDToken(*out.AuthenticationResult.IdToken)
	if err != nil {
		return Identity{}, errors.New("code was not accepted")
	}
	return Identity{Subject: subject, Email: email}, nil
}

// Confirm checks the code and returns the Cognito subject from the ID token.
// The signature is not checked here. The token was just returned by Cognito in this process.
func (c *Cognito) Confirm(ctx context.Context, email, session, code string) (Identity, error) {
	email = strings.TrimSpace(email)
	code = strings.TrimSpace(code)
	if !validEmail(email) || session == "" || code == "" {
		return Identity{}, errors.New("code was not accepted")
	}
	if rest, ok := strings.CutPrefix(session, signUpPrefix); ok {
		return c.confirmSignUp(ctx, email, rest, code)
	}
	out, err := c.api.RespondToAuthChallenge(ctx, &cip.RespondToAuthChallengeInput{
		ClientId:      aws.String(c.clientID),
		ChallengeName: types.ChallengeNameTypeEmailOtp,
		Session:       aws.String(session),
		ChallengeResponses: map[string]string{
			"USERNAME":       email,
			"EMAIL_OTP_CODE": code,
		},
	})
	if err != nil || out.AuthenticationResult == nil || out.AuthenticationResult.IdToken == nil {
		return Identity{}, errors.New("code was not accepted")
	}
	subject, err := subjectFromIDToken(*out.AuthenticationResult.IdToken)
	if err != nil {
		return Identity{}, errors.New("code was not accepted")
	}
	return Identity{Subject: subject, Email: email}, nil
}

func (c *Cognito) emailSession(ctx context.Context, email string, name types.ChallengeNameType, session *string) (string, error) {
	if name == types.ChallengeNameTypeEmailOtp {
		return sessionValue(session)
	}
	if name != types.ChallengeNameTypeSelectChallenge || session == nil || *session == "" {
		return "", errors.New("could not send a code")
	}
	out, err := c.api.RespondToAuthChallenge(ctx, &cip.RespondToAuthChallengeInput{
		ClientId:      aws.String(c.clientID),
		ChallengeName: types.ChallengeNameTypeSelectChallenge,
		Session:       session,
		ChallengeResponses: map[string]string{
			"USERNAME": email,
			"ANSWER":   "EMAIL_OTP",
		},
	})
	if err != nil || out.ChallengeName != types.ChallengeNameTypeEmailOtp {
		return "", errors.New("could not send a code")
	}
	return sessionValue(out.Session)
}

func sessionValue(session *string) (string, error) {
	if session == nil || *session == "" {
		return "", errors.New("could not send a code")
	}
	return *session, nil
}

func validEmail(email string) bool {
	if email == "" || len(email) > 254 || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	at := strings.LastIndex(email, "@")
	return at > 0 && at < len(email)-1 && strings.Contains(email[at+1:], ".")
}

func subjectFromIDToken(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return "", errors.New("cognito token is not valid")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil || claims.Sub == "" {
		return "", errors.New("cognito token has no subject")
	}
	return claims.Sub, nil
}
