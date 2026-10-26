# tflint --recursive from deploy/terraform/ (task tf:lint). The terraform ruleset is bundled with tflint,
# so nothing is downloaded. Local modules declare their providers' sources without versions (the
# root pins them) and no required_version, so the two rules that ask for those are off.
config {
  format = "compact"
}

plugin "terraform" {
  enabled = true
  preset  = "all"
}

rule "terraform_required_version" {
  enabled = false
}

rule "terraform_required_providers" {
  enabled = false
}

# The root splits files by concern (storage.tf, routing.tf, secrets.tf) and takes no variables;
# the local modules keep main.tf, variables.tf and outputs.tf.
rule "terraform_standard_module_structure" {
  enabled = false
}
