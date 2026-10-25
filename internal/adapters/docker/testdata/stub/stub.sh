#!/bin/sh
# Plays one scripted task; the brief in /goal/spec.json picks the script.
brief=$(grep -o '"brief":"[a-z]*"' /goal/spec.json | cut -d'"' -f4)
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

echo "{\"type\":\"started\",\"at\":\"$(now)\",\"session_id\":\"stub\",\"model\":\"stub\"}"
echo "not a protocol line"

case "$brief" in
succeed)
	if [ -z "${ANTHROPIC_API_KEY+x}" ]; then key=absent; else key=present; fi
	if touch /rootfs-probe 2>/dev/null; then rootfs=writable; else rootfs=readonly; fi
	if echo done >/workspace/stub.txt; then ws=writable; else ws=readonly; fi
	if touch /goal/probe 2>/dev/null; then goal=writable; else goal=readonly; fi
	prompt=$(cat /goal/agent/prompt.md)
	echo "{\"type\":\"tool_use\",\"at\":\"$(now)\",\"id\":\"t1\",\"tool\":\"Bash\",\"input\":\"probe\"}"
	echo "{\"type\":\"tool_result\",\"at\":\"$(now)\",\"id\":\"t1\",\"is_error\":false,\"summary\":\"api_key=$key uid=$(id -u) rootfs=$rootfs workspace=$ws goal=$goal home=$HOME prompt=$prompt\"}"
	echo "{\"type\":\"message\",\"at\":\"$(now)\",\"text\":\"token is $CLAUDE_CODE_OAUTH_TOKEN and gateway $GOAL_GATEWAY_TOKEN\"}"
	echo "{\"type\":\"usage\",\"at\":\"$(now)\",\"input_tokens\":10,\"output_tokens\":5}"
	echo "{\"type\":\"finished\",\"at\":\"$(now)\",\"status\":\"succeeded\",\"output\":{\"ok\":true},\"cost_usd\":0.25,\"turns\":2,\"input_tokens\":10,\"output_tokens\":5,\"session_id\":\"stub\"}"
	;;
netprobe)
	# What the task's network reaches, one message per probe: ok-<body>, an HTTP status, or fail.
	# PROBE_SECRET_PORT and PROBE_EXTERNAL_PORT are host loopback listeners, PROBE_HOST_IPS the
	# addresses the host and its bridges have from a container.
	gw=$(grep -o '"gateway_url":"[^"]*"' /goal/spec.json | cut -d'"' -f4)
	proxy=${HTTP_PROXY#http://}
	say() { echo "{\"type\":\"message\",\"at\":\"$(now)\",\"text\":\"probe $1=$2\"}"; }
	result() {
		if out=$("$@" 2>&1); then echo "ok-$out"; return; fi
		code=$(echo "$out" | grep -o 'HTTP/1.[01] [0-9]*' | head -n 1 | cut -d' ' -f2)
		echo "${code:-fail}"
	}
	direct() { result wget -Y off -q -T 3 -O - "$1"; }
	proxied() { result wget -q -T 5 -O - "$1"; }
	tunnel() {
		printf 'CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\nGET /hello HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n' "$1" "$1" |
			nc -w 5 "${proxy%:*}" "${proxy#*:}" 2>/dev/null | grep -o -e 'HTTP/1.1 [0-9]*' -e '^external' | tr '\n' ' ' | sed 's/HTTP\/1.1 //g; s/ *$//; s/ /,/g'
	}
	say direct-host-name "$(direct "http://host.docker.internal:$PROBE_SECRET_PORT/")"
	for ip in $PROBE_HOST_IPS; do say "direct-$ip" "$(direct "http://$ip:$PROBE_SECRET_PORT/")"; done
	say direct-metadata "$(direct http://169.254.169.254/latest/meta-data/)"
	say proxy-host "$(proxied "http://host.docker.internal:$PROBE_SECRET_PORT/")"
	for ip in $PROBE_HOST_IPS; do say "proxy-$ip" "$(proxied "http://$ip:$PROBE_SECRET_PORT/")"; done
	say proxy-metadata "$(proxied http://169.254.169.254/latest/meta-data/)"
	say proxy-external "$(proxied "http://host.docker.internal:$PROBE_EXTERNAL_PORT/hello")"
	say tunnel-external "$(tunnel "host.docker.internal:$PROBE_EXTERNAL_PORT")"
	say tunnel-metadata "$(tunnel 169.254.169.254:80)"
	say tunnel-host "$(tunnel "host.docker.internal:$PROBE_SECRET_PORT")"
	say gateway "$(result wget -Y off -q -T 5 -O - --header "Authorization: Bearer $GOAL_GATEWAY_TOKEN" --post-data '{}' "$gw")"
	say gateway-run "$(result wget -Y off -q -T 5 -O - --header "Authorization: Bearer $GOAL_GATEWAY_TOKEN" "${gw%/mcp}/run")"
	say goald-api "$(direct "http://$proxy/v1/me")"
	echo "{\"type\":\"finished\",\"at\":\"$(now)\",\"status\":\"succeeded\",\"output\":{\"ok\":true},\"turns\":1,\"session_id\":\"stub\"}"
	;;
sleep)
	exec sleep 600
	;;
crash)
	echo "boom: token $CLAUDE_CODE_OAUTH_TOKEN" >&2
	exit 3
	;;
esac
