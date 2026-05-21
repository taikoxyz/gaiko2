#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "${SCRIPT_DIR}/.." && pwd)
COMPOSE_FILE="${REPO_ROOT}/compose.yaml"
ENV_TEMPLATE="${REPO_ROOT}/.env.example"

FORK=""
RELEASE=""
DEPLOY_ROOT="${REPO_ROOT}/deploy"
OVERRIDE_TEE_IMAGE=""
OVERRIDE_PCCS_HOST=""
OVERRIDE_PORT=""
OVERRIDE_INSTANCE_ID=""
OVERRIDE_REGISTER_HOOK=""

usage() {
    cat <<'EOF'
gaiko2 tee deploy helper

Usage:
  ./scripts/deploy-tee.sh --fork <fork> --release <release> <command> [options]

Commands:
  init       Create the release directory and bootstrap the tee key
  metadata   Print the copied release attestation metadata
  register   Run the optional external register hook
  up         Start the tee server for this release
  logs       Follow the tee server logs
  status     Show release files and compose status
  health     Query the local /healthz endpoint
  down       Stop and remove the tee server for this release

Global options:
  --fork <fork>                Fork name, e.g. shasta
  --release <release>          Release name, e.g. v1.0.0
  --deploy-root <path>         Deploy root, defaults to ./deploy
  --tee-image <image>          Override GAIKO2_TEE_IMAGE in the release env
  --pccs-host <host:port>      Override PCCS_HOST in the release env
  --port <port>                Override GAIKO2_TEE_PORT in the release env
  --instance-id <id>           Override GAIKO2_INSTANCE_ID in the release env
  --register-hook <path>       Override GAIKO2_REGISTER_HOOK in the release env
  -h, --help                   Show this help
EOF
}

log() {
    printf '%s\n' "$*"
}

die() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

slugify() {
    printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-+//; s/-+$//'
}

ensure_release_args() {
    [[ -n "${FORK}" ]] || die "--fork is required"
    [[ -n "${RELEASE}" ]] || die "--release is required"
    validate_path_component "fork" "${FORK}"
    validate_path_component "release" "${RELEASE}"
}

validate_path_component() {
    local label="$1"
    local value="$2"
    if [[ ! "${value}" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
        die "${label} must match ^[A-Za-z0-9][A-Za-z0-9._-]*$: ${value}"
    fi
}

release_dir() {
    printf '%s/%s/%s' "${DEPLOY_ROOT}" "${FORK}" "${RELEASE}"
}

release_config_dir() {
    printf '%s/config' "$(release_dir)"
}

release_secret_dir() {
    printf '%s/secrets' "$(release_dir)"
}

release_env_file() {
    printf '%s/.env' "$(release_dir)"
}

release_bootstrap_json() {
    printf '%s/bootstrap.gaiko2.json' "$(release_config_dir)"
}

release_registered_json() {
    printf '%s/registered.gaiko2.json' "$(release_config_dir)"
}

release_attestation_json() {
    printf '%s/attestation.gaiko2.json' "$(release_config_dir)"
}

release_private_key() {
    printf '%s/priv.gaiko2.key' "$(release_secret_dir)"
}

compose_project_name() {
    printf 'gaiko2-%s-%s' "$(slugify "${FORK}")" "$(slugify "${RELEASE}")"
}

docker_compose() {
    docker compose \
        --project-name "$(compose_project_name)" \
        --env-file "$(release_env_file)" \
        -f "${COMPOSE_FILE}" \
        "$@"
}

escape_sed_replacement() {
    printf '%s' "$1" | sed -e 's/[&|]/\\&/g'
}

upsert_env() {
    local file="$1"
    local key="$2"
    local value="$3"
    local escaped
    if [[ "${value}" == *$'\n'* || "${value}" == *$'\r'* ]]; then
        die "env value for ${key} must not contain newlines"
    fi
    escaped=$(escape_sed_replacement "${value}")
    if grep -q "^${key}=" "${file}"; then
        sed -i "s|^${key}=.*$|${key}=${escaped}|" "${file}"
    else
        printf '%s=%s\n' "${key}" "${value}" >>"${file}"
    fi
}

ensure_release_dirs() {
    mkdir -p "$(release_config_dir)" "$(release_secret_dir)"
}

ensure_release_env() {
    local file
    file=$(release_env_file)
    if [[ ! -f "${file}" ]]; then
        cp "${ENV_TEMPLATE}" "${file}"
    fi

    upsert_env "${file}" "GAIKO2_FORK" "${FORK}"
    upsert_env "${file}" "GAIKO2_CONFIG_DIR_HOST" "$(release_config_dir)"
    upsert_env "${file}" "GAIKO2_SECRET_DIR_HOST" "$(release_secret_dir)"

    if [[ -n "${OVERRIDE_TEE_IMAGE}" ]]; then
        upsert_env "${file}" "GAIKO2_TEE_IMAGE" "${OVERRIDE_TEE_IMAGE}"
    fi
    if [[ -n "${OVERRIDE_PCCS_HOST}" ]]; then
        upsert_env "${file}" "PCCS_HOST" "${OVERRIDE_PCCS_HOST}"
    fi
    if [[ -n "${OVERRIDE_PORT}" ]]; then
        upsert_env "${file}" "GAIKO2_TEE_PORT" "${OVERRIDE_PORT}"
    fi
    if [[ -n "${OVERRIDE_INSTANCE_ID}" ]]; then
        upsert_env "${file}" "GAIKO2_INSTANCE_ID" "${OVERRIDE_INSTANCE_ID}"
    fi
    if [[ -n "${OVERRIDE_REGISTER_HOOK}" ]]; then
        upsert_env "${file}" "GAIKO2_REGISTER_HOOK" "${OVERRIDE_REGISTER_HOOK}"
    fi
}

sync_existing_release_env() {
    local file
    file=$(release_env_file)
    [[ -f "${file}" ]] || return 0
    upsert_env "${file}" "GAIKO2_FORK" "${FORK}"
    upsert_env "${file}" "GAIKO2_CONFIG_DIR_HOST" "$(release_config_dir)"
    upsert_env "${file}" "GAIKO2_SECRET_DIR_HOST" "$(release_secret_dir)"

    if [[ -n "${OVERRIDE_TEE_IMAGE}" ]]; then
        upsert_env "${file}" "GAIKO2_TEE_IMAGE" "${OVERRIDE_TEE_IMAGE}"
    fi
    if [[ -n "${OVERRIDE_PCCS_HOST}" ]]; then
        upsert_env "${file}" "PCCS_HOST" "${OVERRIDE_PCCS_HOST}"
    fi
    if [[ -n "${OVERRIDE_PORT}" ]]; then
        upsert_env "${file}" "GAIKO2_TEE_PORT" "${OVERRIDE_PORT}"
    fi
    if [[ -n "${OVERRIDE_INSTANCE_ID}" ]]; then
        upsert_env "${file}" "GAIKO2_INSTANCE_ID" "${OVERRIDE_INSTANCE_ID}"
    fi
    if [[ -n "${OVERRIDE_REGISTER_HOOK}" ]]; then
        upsert_env "${file}" "GAIKO2_REGISTER_HOOK" "${OVERRIDE_REGISTER_HOOK}"
    fi
}

load_release_env() {
    local file
    file=$(release_env_file)
    [[ -f "${file}" ]] || die "release env missing: ${file}"
    while IFS= read -r line || [[ -n "${line}" ]]; do
        line="${line#"${line%%[![:space:]]*}"}"
        line="${line%"${line##*[![:space:]]}"}"
        [[ -z "${line}" || "${line}" == \#* ]] && continue
        [[ "${line}" == *=* ]] || die "invalid env line in ${file}: ${line}"
        local key="${line%%=*}"
        local value="${line#*=}"
        [[ "${key}" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || die "invalid env key in ${file}: ${key}"
        case "${key}" in
        GAIKO2_TEE_IMAGE|GAIKO2_NATIVE_IMAGE|GAIKO2_EGO_SIGNING_KEY|GAIKO2_TEE_PORT|GAIKO2_NATIVE_PORT|GAIKO2_CONFIG_DIR_HOST|GAIKO2_SECRET_DIR_HOST|GAIKO2_TEE_TYPE|GAIKO2_FORK|GAIKO2_INSTANCE_ID|GAIKO2_REGISTER_HOOK|GAIKO2_API_KEY|GAIKO2_MAX_BODY_BYTES|GAIKO2_ALLOW_INSECURE_PCCS|PCCS_HOST)
            export "${key}=${value}"
            ;;
        *)
            ;;
        esac
    done <"${file}"
}

resolve_hook() {
    if [[ -n "${OVERRIDE_REGISTER_HOOK}" ]]; then
        printf '%s' "${OVERRIDE_REGISTER_HOOK}"
        return
    fi
    if [[ -n "${GAIKO2_REGISTER_HOOK:-}" ]]; then
        printf '%s' "${GAIKO2_REGISTER_HOOK}"
        return
    fi
    printf '%s' ""
}

require_bootstrap() {
    local path
    path=$(release_bootstrap_json)
    [[ -f "${path}" ]] || die "bootstrap output missing: ${path}. Run init first."
}

require_release_env() {
    [[ -f "$(release_env_file)" ]] || die "release env missing: $(release_env_file). Run init first."
}

require_release_identity() {
    local key_path registered_path
    key_path=$(release_private_key)
    registered_path=$(release_registered_json)
    [[ -f "${key_path}" ]] || die "sealed key missing: ${key_path}. Run init first."
    [[ -n "${GAIKO2_API_KEY:-}" ]] || die "api key is unresolved. Set GAIKO2_API_KEY in $(release_env_file)."
    if [[ -z "${GAIKO2_INSTANCE_ID:-}" && ! -f "${registered_path}" ]]; then
        die "instance id is unresolved. Set GAIKO2_INSTANCE_ID or write ${registered_path}."
    fi
}

cmd_init() {
    ensure_release_dirs
    ensure_release_env
    log "release dir: $(release_dir)"
    log "compose project: $(compose_project_name)"
    log "bootstrapping tee release"
    docker_compose --profile tee-init run --rm gaiko2-tee-init
}

cmd_register() {
    require_release_env
    sync_existing_release_env
    load_release_env
    require_bootstrap

    local hook
    hook=$(resolve_hook)
    if [[ -z "${hook}" ]]; then
        log "no register hook configured"
        log "bootstrap quote: $(release_bootstrap_json)"
        log "expected registered ids file: $(release_registered_json)"
        return 0
    fi
    [[ -x "${hook}" ]] || die "register hook is not executable: ${hook}"

    export GAIKO2_DEPLOY_DIR="$(release_dir)"
    export GAIKO2_CONFIG_DIR="$(release_config_dir)"
    export GAIKO2_SECRET_DIR="$(release_secret_dir)"
    export GAIKO2_BOOTSTRAP_JSON="$(release_bootstrap_json)"
    export GAIKO2_REGISTERED_JSON="$(release_registered_json)"
    export GAIKO2_ATTESTATION_JSON="$(release_attestation_json)"
    export GAIKO2_FORK="${FORK}"
    export GAIKO2_RELEASE="${RELEASE}"

    log "running register hook: ${hook}"
    "${hook}"

    if [[ -z "${GAIKO2_INSTANCE_ID:-}" && ! -f "$(release_registered_json)" ]]; then
        die "register hook completed without creating $(release_registered_json) or setting GAIKO2_INSTANCE_ID"
    fi
    log "registration state is ready"
}

cmd_up() {
    require_release_env
    sync_existing_release_env
    load_release_env
    require_release_identity
    log "starting tee service for $(compose_project_name)"
    if ! docker_compose --profile tee up -d --wait gaiko2-tee; then
        log "startup failed; inspect logs with:"
        log "  ./scripts/deploy-tee.sh --fork ${FORK} --release ${RELEASE} logs"
        return 1
    fi
}

cmd_logs() {
    require_release_env
    sync_existing_release_env
    docker_compose logs -f --tail=200 gaiko2-tee
}

cmd_status() {
    local env_status bootstrap_status key_status registered_status attestation_status
    env_status=missing
    bootstrap_status=missing
    key_status=missing
    registered_status=missing
    attestation_status=missing

    [[ -f "$(release_env_file)" ]] && env_status=present
    [[ -f "$(release_bootstrap_json)" ]] && bootstrap_status=present
    [[ -f "$(release_private_key)" ]] && key_status=present
    [[ -f "$(release_registered_json)" ]] && registered_status=present
    [[ -f "$(release_attestation_json)" ]] && attestation_status=present

    log "release dir: $(release_dir)"
    log "compose project: $(compose_project_name)"
    log "env: ${env_status}"
    log "bootstrap: ${bootstrap_status}"
    log "attestation: ${attestation_status}"
    log "sealed key: ${key_status}"
    log "registered: ${registered_status}"

    if [[ "${env_status}" == "present" ]]; then
        load_release_env
        log "port: ${GAIKO2_TEE_PORT:-8080}"
        log "image: ${GAIKO2_TEE_IMAGE:-gaiko2-tee:latest}"
        log "register hook: ${GAIKO2_REGISTER_HOOK:-}"
        docker_compose ps || true
    fi
}

cmd_health() {
    require_release_env
    sync_existing_release_env
    load_release_env
    local port="${GAIKO2_TEE_PORT:-8080}"
    curl -fsS "http://127.0.0.1:${port}/healthz"
}

cmd_metadata() {
    local path
    path=$(release_attestation_json)
    [[ -f "${path}" ]] || die "attestation metadata missing: ${path}. Run init first."
    cat "${path}"
}

cmd_down() {
    require_release_env
    sync_existing_release_env
    docker_compose stop gaiko2-tee || true
    docker_compose rm -sf gaiko2-tee
}

main() {
    local command=""

    while [[ $# -gt 0 ]]; do
        case "$1" in
        --fork)
            FORK="${2:-}"
            shift 2
            ;;
        --release)
            RELEASE="${2:-}"
            shift 2
            ;;
        --deploy-root)
            DEPLOY_ROOT="${2:-}"
            shift 2
            ;;
        --tee-image)
            OVERRIDE_TEE_IMAGE="${2:-}"
            shift 2
            ;;
        --pccs-host)
            OVERRIDE_PCCS_HOST="${2:-}"
            shift 2
            ;;
        --port)
            OVERRIDE_PORT="${2:-}"
            shift 2
            ;;
        --instance-id)
            OVERRIDE_INSTANCE_ID="${2:-}"
            shift 2
            ;;
        --register-hook)
            OVERRIDE_REGISTER_HOOK="${2:-}"
            shift 2
            ;;
        -h|--help)
            usage
            return 0
            ;;
        init|metadata|register|up|logs|status|health|down)
            if [[ -n "${command}" ]]; then
                die "unexpected extra command: $1"
            fi
            command="$1"
            shift
            ;;
        *)
            die "unknown argument: $1"
            ;;
        esac
    done

    [[ -n "${command}" ]] || {
        usage
        return 1
    }

    ensure_release_args

    case "${command}" in
    init)
        cmd_init
        ;;
    metadata)
        cmd_metadata
        ;;
    register)
        cmd_register
        ;;
    up)
        cmd_up
        ;;
    logs)
        cmd_logs
        ;;
    status)
        cmd_status
        ;;
    health)
        cmd_health
        ;;
    down)
        cmd_down
        ;;
    *)
        die "unsupported command: ${command}"
        ;;
    esac
}

if [[ "${GAIKO2_TEST_SOURCE_ONLY:-0}" != "1" ]]; then
    main "$@"
fi
