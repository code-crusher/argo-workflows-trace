Description: Add AWS RDS IAM authentication for MySQL and MariaDB
Authors: [code-crusher](https://github.com/code-crusher)
Component: General
Issues: 17135

You can now authenticate the controller to AWS RDS for MySQL, RDS for MariaDB and Aurora MySQL using IAM authentication tokens instead of a static password.
Set `awsRDSToken` in the `mysql` persistence configuration, the same way as for `postgresql`.
The controller builds a fresh token for each new database connection using the default AWS credential chain, which supports IAM Roles for Service Accounts (IRSA), instance profiles and other standard mechanisms.
The `region` field is optional and is auto-detected from the environment if omitted.
RDS sends IAM tokens using MySQL's cleartext authentication plugin, so you must enable TLS with the `tls` connection option.
When `awsRDSToken` is enabled no `passwordSecret` is read, but you must still provide a `userNameSecret`.
This is part of the shared database configuration, so it also covers node status offloading and database synchronization.
