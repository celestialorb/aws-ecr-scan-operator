# AWS ECR Continuous Scan Operator
A basic operator to control image scans in AWS ECR on a schedule.

## Motivation
While AWS ECR has the ability to specify scanning on a schedule, this feature set does not exist in GovCloud. I wanted a way to periodically trigger scans on a set of images so that, in combination with an AWS ECR Prometheus exporter, I can be kept up-to-date about the results of these scans.

## Implementation
This project makes use of `chrono`, a Golang scheduler as well as the AWS SDK (v2) for Golang to get information about repositories and images and to trigger image scans. For configuration this project uses `viper`. For observability this project uses `logrus` for logging as well as `http` and the Prometheus Golang modules for exposing metrics.

## Usage
Given the small scope of this operator, configuring it is relatively simple.
All configuration is done via environment variables that are prefixed with `AWS_ECR_SCAN`, with a following `_` to separate the namespace from the configuration element.

Below is a table of all current configuration elements.

| Element | Environment Variable | Default | Values | Description |
| --- | --- | --- | --- | --- |
| `cron.schedule` | `AWS_ECR_SCAN_CRON_SCHEDULE` | `0 0 0 * * *` | N/A | The cron schedule for triggering the scan operator. |
| `images.filter.tag.status` | `AWS_ECR_SCAN_IMAGES_FILTER_TAG_STATUS` | `any` | `any`,`tagged`,`untagged` | Filter images to trigger scans on by tag status. |
| `log.format` | `AWS_ECR_SCAN_LOG_FORMAT` | `logfmt` | `json`,`logfmt`,`text` | The format of the logging output. |
| `log.level` | `AWS_ECR_SCAN_LOG_LEVEL` | `info` | `debug`,`info`,`warn`,`error`,`fatal` | The log level for the logging output. |
| `web.host` | `AWS_ECR_SCAN_WEB_HOST` | `127.0.0.1` | N/A | The host to bind to for the webserver. |
| `web.port` | `AWS_ECR_SCAN_WEB_PORT` | `2112` | N/A | The port to bind to for the webserver. |

## Permissions
Since this operator interacts with the AWS ECR API it will need to run under a role with the proper AWS IAM permissions in order to perform the necessary operations. Below is a list of all permissions this operators needs to be permitted to do.

| AWS IAM Action |
| --- |
| `ecr:DescribeRepositories` |
| `ecr:ListImages` |
| `ecr:StartImageScan` |

## Metrics
This operator comes with a webserver to export some simple Prometheus metrics to track its operation in addition to the standard Golang Prometheus metrics. The table below describes the metrics exported.

| Name | Type | Description |
| --- | --- | --- |
| `aws_ecr_scans_requested` | Counter | The total count of AWS ECR image scan requests sent. |
| `aws_ecr_scans_requested_errors` | Counter | The total count of AWS ECR image scan requests that results in an error. |
| `aws_ecr_scans_rate_limited` | Counter | The total count of AWS ECR image scan requests rejected due to rate-limiting. |