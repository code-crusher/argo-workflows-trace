package sqldb

import (
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"github.com/argoproj/argo-workflows/v4/config"
	"github.com/argoproj/argo-workflows/v4/util/logging"
)

func rdsMySQLConfig(options map[string]string) *config.MySQLConfig {
	return &config.MySQLConfig{
		DatabaseConfig: config.DatabaseConfig{
			Host:     "mydb.abc123.us-east-1.rds.amazonaws.com",
			Port:     3306,
			Database: "argo",
		},
		Options: options,
		AWSRDSToken: &config.AWSRDSTokenConfig{
			Enabled: true,
			Region:  "us-east-1",
		},
	}
}

// useStaticAWSCredentials makes the default AWS credential chain resolve
// credentials without network access, so tokens can be built in tests.
func useStaticAWSCredentials(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_REGION", "us-east-1")
}

func TestBuildMySQLConfigWithAWSRDSDisabled(t *testing.T) {
	cfg := rdsMySQLConfig(map[string]string{"tls": "true"})
	cfg.AWSRDSToken.Enabled = false
	_, err := buildMySQLConfigWithAWSRDS(cfg, "argo", 5*time.Second)
	require.ErrorContains(t, err, "awsRDSToken must be enabled")
}

// IAM authentication needs the cleartext plugin, which go-sql-driver only
// permits over TLS. Without TLS the token would be sent in the clear, so the
// config must be rejected rather than silently allowing it.
func TestBuildMySQLConfigWithAWSRDSRequiresTLS(t *testing.T) {
	_, err := buildMySQLConfigWithAWSRDS(rdsMySQLConfig(nil), "argo", 5*time.Second)
	require.ErrorContains(t, err, "TLS must be enabled")

	// "preferred" silently falls back to plaintext when the server does not
	// offer TLS, which is not safe for a bearer token either.
	_, err = buildMySQLConfigWithAWSRDS(rdsMySQLConfig(map[string]string{"tls": "preferred"}), "argo", 5*time.Second)
	require.ErrorContains(t, err, "TLS must be enabled")

	_, err = buildMySQLConfigWithAWSRDS(rdsMySQLConfig(map[string]string{"tls": "false"}), "argo", 5*time.Second)
	require.ErrorContains(t, err, "TLS must be enabled")
}

func TestBuildMySQLConfigWithAWSRDSTLSEnabled(t *testing.T) {
	for _, tlsValue := range []string{"true", "skip-verify"} {
		t.Run(tlsValue, func(t *testing.T) {
			mysqlCfg, err := buildMySQLConfigWithAWSRDS(
				rdsMySQLConfig(map[string]string{"tls": tlsValue}), "argo", 5*time.Second)
			require.NoError(t, err)
			require.NotNil(t, mysqlCfg.TLS)
			assert.False(t, mysqlCfg.AllowFallbackToPlaintext)
			assert.True(t, mysqlCfg.AllowCleartextPasswords, "RDS IAM tokens use the cleartext plugin")
			assert.Equal(t, "argo", mysqlCfg.User)
			assert.Equal(t, "mydb.abc123.us-east-1.rds.amazonaws.com:3306", mysqlCfg.Addr)
			// No static password is read from a secret in IAM mode.
			assert.Empty(t, mysqlCfg.Passwd)
		})
	}
}

// The driver invokes the BeforeConnect hook for every new connection, which is
// what keeps the (15 minute) token fresh.
func TestSetMySQLRDSIAMPassword(t *testing.T) {
	useStaticAWSCredentials(t)

	ctx := logging.TestContext(t.Context())
	connCfg := &mysql.Config{Addr: "mydb.abc123.us-east-1.rds.amazonaws.com:3306"}

	require.NoError(t, setMySQLRDSIAMPassword(ctx, connCfg, "us-east-1", "argo"))

	// RDS tokens are presigned URLs carrying the IAM user and the connect action.
	assert.Contains(t, connCfg.Passwd, "X-Amz-Signature=")
	assert.Contains(t, connCfg.Passwd, "DBUser=argo")
	assert.Contains(t, connCfg.Passwd, "Action=connect")
	assert.Contains(t, connCfg.Passwd, "mydb.abc123.us-east-1.rds.amazonaws.com")
}

func TestSetMySQLRDSIAMPasswordRequiresPort(t *testing.T) {
	useStaticAWSCredentials(t)

	ctx := logging.TestContext(t.Context())
	err := setMySQLRDSIAMPassword(ctx, &mysql.Config{Addr: "mydb.abc123.us-east-1.rds.amazonaws.com"}, "", "argo")
	require.ErrorContains(t, err, "missing a port")
}

// With awsRDSToken enabled the session must be built from the username secret
// only: reading a password secret that does not exist would fail the connection
// before IAM auth is ever attempted.
func TestCreateMySQLDBSessionWithAWSRDSDoesNotReadPasswordSecret(t *testing.T) {
	useStaticAWSCredentials(t)

	ctx := logging.TestContext(t.Context())
	client := kubefake.NewClientset(&apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "argo-mysql-config", Namespace: "argo"},
		Data:       map[string][]byte{"username": []byte("argo")},
	})

	cfg := rdsMySQLConfig(map[string]string{"tls": "true"})
	// Use a host that refuses connections immediately so the test does not
	// depend on DNS or on reaching AWS.
	cfg.Host = "127.0.0.1"
	cfg.Port = 1
	cfg.UsernameSecret = apiv1.SecretKeySelector{
		LocalObjectReference: apiv1.LocalObjectReference{Name: "argo-mysql-config"},
		Key:                  "username",
	}
	// Deliberately left unset: in IAM mode it must not be required.
	cfg.PasswordSecret = apiv1.SecretKeySelector{}

	// The connection fails (nothing is listening) but it must fail while
	// connecting, not while looking up a password.
	_, err := createMySQLDBSession(ctx, client, "argo", cfg, nil, 2*time.Second)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "passwordSecret")
	assert.NotContains(t, err.Error(), "does not have the key")
}

func TestCreateMySQLDBSessionWithAWSRDSRequiresTLS(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	client := kubefake.NewClientset(&apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "argo-mysql-config", Namespace: "argo"},
		Data:       map[string][]byte{"username": []byte("argo")},
	})

	cfg := rdsMySQLConfig(nil)
	cfg.UsernameSecret = apiv1.SecretKeySelector{
		LocalObjectReference: apiv1.LocalObjectReference{Name: "argo-mysql-config"},
		Key:                  "username",
	}

	_, err := createMySQLDBSession(ctx, client, "argo", cfg, nil, 2*time.Second)
	require.ErrorContains(t, err, "TLS must be enabled")
}

// The BeforeConnect hook must be wired into the connector the session uses, so
// every new connection builds its own token. Without AWS credentials the token
// build fails, and that failure (rather than a dial error) proves the hook runs
// on each Connect, before the driver dials.
func TestMySQLRDSConnectorBuildsTokenPerConnection(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	cfg := rdsMySQLConfig(map[string]string{"tls": "true"})
	cfg.Host = "127.0.0.1"
	cfg.Port = 1
	mysqlCfg, err := buildMySQLConfigWithAWSRDS(cfg, "argo", time.Second)
	require.NoError(t, err)

	connector, err := mysql.NewConnector(mysqlCfg)
	require.NoError(t, err)

	for i := range 2 {
		_, err := connector.Connect(logging.TestContext(t.Context()))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to build RDS auth token", "connection %d", i)
	}
}

// A non-IAM MySQL configuration must keep reading the password secret.
func TestCreateMySQLDBSessionStillReadsPasswordSecret(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	client := kubefake.NewClientset(&apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "argo-mysql-config", Namespace: "argo"},
		Data:       map[string][]byte{"username": []byte("argo")},
	})

	cfg := &config.MySQLConfig{
		DatabaseConfig: config.DatabaseConfig{
			Host:     "127.0.0.1",
			Port:     1,
			Database: "argo",
			UsernameSecret: apiv1.SecretKeySelector{
				LocalObjectReference: apiv1.LocalObjectReference{Name: "argo-mysql-config"},
				Key:                  "username",
			},
			PasswordSecret: apiv1.SecretKeySelector{
				LocalObjectReference: apiv1.LocalObjectReference{Name: "argo-mysql-config"},
				Key:                  "password",
			},
		},
	}

	_, err := createMySQLDBSession(ctx, client, "argo", cfg, nil, 2*time.Second)
	require.ErrorContains(t, err, "does not have the key 'password'")
}
