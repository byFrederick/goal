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
sleep)
	exec sleep 600
	;;
crash)
	echo "boom: token $CLAUDE_CODE_OAUTH_TOKEN" >&2
	exit 3
	;;
esac
