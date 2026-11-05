terraform {
  required_providers {
    strettchcloud = {
      source = "strettch-cloud/strettchcloud"
    }
  }
}

provider "strettchcloud" {
  # api_key defaults to the STRETTCH_CLOUD_API_KEY environment variable.
  # api_url defaults to the production API.
}
