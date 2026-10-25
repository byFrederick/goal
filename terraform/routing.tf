# The public host and the route that sends all of it to the control Worker (spec 4 §5.12: every
# public path goes Worker -> goald container). The Worker has no origin server, so the record is
# the documented placeholder for Worker-only hosts: a proxied AAAA to the discard prefix 100::.
#
# The route names a Worker that wrangler deploys (deploy/cloudflare), and Cloudflare refuses a
# route to a Worker that does not exist yet: on a new install apply everything else first, deploy
# the Worker, then apply again (runbook, bootstrap). The wrangler config declares no routes of its
# own, so this is the only one.
resource "cloudflare_dns_record" "goal" {
  zone_id = data.cloudflare_zone.main.id
  name    = local.settings.host
  type    = "AAAA"
  content = "100::"
  proxied = true
  ttl     = 1 # automatic, as every proxied record
  comment = "${local.managed_by}: served by the ${local.settings.worker} Worker"
}

resource "cloudflare_workers_route" "goal" {
  zone_id = data.cloudflare_zone.main.id
  pattern = "${local.settings.host}/*"
  script  = local.settings.worker
}
