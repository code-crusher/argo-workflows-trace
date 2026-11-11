package sqldb

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

type awsRDSConnector struct {
	dsn      string
	endpoint string
	username string
	region   string
}

// buildAWSRDSToken builds an RDS IAM authentication token for the given endpoint,
// using the default AWS credential chain. It is shared by the PostgreSQL and MySQL
// connectors so both resolve credentials and regions the same way.
func buildAWSRDSToken(ctx context.Context, endpoint, region, username string) (string, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return "", fmt.Errorf("failed to load AWS config: %w", err)
	}

	token, err := auth.BuildAuthToken(ctx, endpoint, awsCfg.Region, username, awsCfg.Credentials)
	if err != nil {
		return "", fmt.Errorf("failed to build RDS auth token: %w", err)
	}
	return token, nil
}

func (c *awsRDSConnector) Connect(ctx context.Context) (driver.Conn, error) {
	token, err := buildAWSRDSToken(ctx, c.endpoint, c.region, c.username)
	if err != nil {
		return nil, err
	}

	// Escape single quotes in token for safe DSN interpolation
	escapedToken := strings.ReplaceAll(token, "'", "\\'")

	dsnWithPassword := fmt.Sprintf("%s password='%s'", c.dsn, escapedToken)

	return pq.Driver{}.Open(dsnWithPassword)
}

func (c *awsRDSConnector) Driver() driver.Driver {
	return pq.Driver{}
}

// setMySQLRDSIAMPassword is the go-sql-driver BeforeConnect hook used for MySQL
// IAM authentication: it builds a fresh RDS token and sets it as the password of
// the connection about to be opened. Tokens expire after 15 minutes, so one is
// built per connection rather than once per session.
func setMySQLRDSIAMPassword(ctx context.Context, connCfg *mysql.Config, region, username string) error {
	token, err := buildAWSRDSToken(ctx, connCfg.Addr, region, username)
	if err != nil {
		return err
	}
	connCfg.Passwd = token
	return nil
}
