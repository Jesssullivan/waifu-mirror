# State is stored locally by default.
# For CI, configure a remote backend via -backend-config or environment.
# See CIVO Object Store docs for S3-compatible backend configuration.
terraform {
  backend "local" {}
}
