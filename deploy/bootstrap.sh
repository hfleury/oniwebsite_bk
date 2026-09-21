#!/usr/bin/env bash
# Idempotent host setup for the Oni Web Officer droplet (Ubuntu LTS).
#
# Run as root, from a checkout that contains this deploy/ directory:
#   ONI_DOMAIN=example.com DEPLOY_PUBKEY='ssh-ed25519 AAAA... ci-deploy' ./bootstrap.sh
#
# Required environment:
#   ONI_DOMAIN     public hostname Caddy serves and gets a certificate for
#   DEPLOY_PUBKEY  ed25519 public key (one line) that CI uses to log in as `deploy`
# Optional environment:
#   ADMIN_USER     human sudo user that replaces root login (default: oniadmin).
#                  Must not match an existing group: Ubuntu ships `admin`, so that name is rejected.
#
# The service is enabled but not started: it needs locales/ and dist/, which
# the first CI deploys provide. /etc/oni/oniweb.env (SENTRY_DSN) is created
# empty and never overwritten; fill it in by hand.
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ONI_DOMAIN="${ONI_DOMAIN:-}"
DEPLOY_PUBKEY="${DEPLOY_PUBKEY:-}"
ADMIN_USER="${ADMIN_USER:-oniadmin}"

APP_ROOT=/opt/oni
SWAPFILE=/swapfile
ENV_DIR=/etc/oni
ENV_FILE="$ENV_DIR/oniweb.env"
SSHD_DROPIN=/etc/ssh/sshd_config.d/00-oni-hardening.conf

log() {
  printf '==> %s\n' "$*"
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

check_environment() {
  [[ $EUID -eq 0 ]] || die "run as root"

  # shellcheck source=/dev/null
  . /etc/os-release
  [[ ${ID:-} == ubuntu ]] || die "this script targets Ubuntu, found ${ID:-unknown}"

  [[ -n $ONI_DOMAIN ]] || die "ONI_DOMAIN is required"
  # Strict hostname check: the value is substituted into the Caddyfile.
  [[ $ONI_DOMAIN =~ ^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$ ]] ||
    die "ONI_DOMAIN is not a valid lowercase hostname: $ONI_DOMAIN"

  [[ -n $DEPLOY_PUBKEY ]] || die "DEPLOY_PUBKEY is required"
  [[ $DEPLOY_PUBKEY =~ ^ssh-ed25519\ [A-Za-z0-9+/=]+(\ [^[:cntrl:]]*)?$ ]] ||
    die "DEPLOY_PUBKEY must be a single-line ssh-ed25519 public key"

  [[ $ADMIN_USER =~ ^[a-z][a-z0-9_-]*$ ]] || die "ADMIN_USER is not a valid username: $ADMIN_USER"
  # useradd creates a same-named group and aborts if one exists (Ubuntu ships `admin`).
  # Catch it here, before any host state changes, instead of failing halfway through.
  if ! id -u "$ADMIN_USER" >/dev/null 2>&1 && getent group "$ADMIN_USER" >/dev/null; then
    die "ADMIN_USER $ADMIN_USER collides with an existing group; choose another name"
  fi
}

install_packages() {
  log "installing packages"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y ca-certificates curl gnupg apt-transport-https ufw unattended-upgrades rsync

  if [[ ! -f /etc/apt/sources.list.d/caddy-stable.list ]]; then
    curl -fsSL 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' |
      gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -fsSL 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
      -o /etc/apt/sources.list.d/caddy-stable.list
    apt-get update
  fi
  apt-get install -y caddy

  cat >/etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
EOF
}

setup_swap() {
  if swapon --show=NAME --noheadings | grep -qx "$SWAPFILE"; then
    return
  fi
  log "creating 1 GB swapfile"
  if [[ ! -f $SWAPFILE ]]; then
    fallocate -l 1G "$SWAPFILE"
    chmod 0600 "$SWAPFILE"
    mkswap "$SWAPFILE"
  fi
  swapon "$SWAPFILE"
  grep -q "^$SWAPFILE " /etc/fstab || echo "$SWAPFILE none swap sw 0 0" >>/etc/fstab
}

create_service_users() {
  log "creating users oni (service) and deploy (CI)"
  id -u oni >/dev/null 2>&1 ||
    useradd --system --no-create-home --shell /usr/sbin/nologin oni
  id -u deploy >/dev/null 2>&1 ||
    useradd --create-home --shell /bin/bash deploy

  install -d -o deploy -g deploy -m 0700 /home/deploy/.ssh
  # `restrict` turns off port/agent/X11 forwarding and pty allocation for this key.
  printf 'restrict %s\n' "$DEPLOY_PUBKEY" >/home/deploy/.ssh/authorized_keys
  chown deploy:deploy /home/deploy/.ssh/authorized_keys
  chmod 0600 /home/deploy/.ssh/authorized_keys
}

create_app_directories() {
  log "creating $APP_ROOT layout"
  install -d -o root -g root -m 0755 "$APP_ROOT"
  # deploy owns these (it writes releases and swaps files); oni only reads via group/other.
  install -d -o deploy -g oni -m 0755 "$APP_ROOT/oniwebsite" "$APP_ROOT/oniwebsite/releases"
  install -d -o deploy -g oni -m 0755 "$APP_ROOT/oniwebsite_bk"
}

install_deploy_scripts() {
  log "installing deploy scripts to /usr/local/sbin"
  install -o root -g root -m 0755 "$DEPLOY_DIR/deploy-frontend" /usr/local/sbin/deploy-frontend
  install -o root -g root -m 0755 "$DEPLOY_DIR/deploy-backend" /usr/local/sbin/deploy-backend
}

# Validate a sudoers fragment with visudo before it goes live, so a typo can't break sudo.
install_sudoers() {
  local name="$1" content="$2" tmp
  tmp="$(mktemp)"
  printf '%s\n' "$content" >"$tmp"
  if ! visudo -cf "$tmp" >/dev/null; then
    rm -f "$tmp"
    die "invalid sudoers content for $name"
  fi
  install -o root -g root -m 0440 "$tmp" "/etc/sudoers.d/$name"
  rm -f "$tmp"
}

configure_deploy_sudo() {
  log "granting deploy: systemctl restart/stop oniweb"
  install_sudoers deploy-oniweb \
    'deploy ALL=(root) NOPASSWD: /usr/bin/systemctl restart oniweb, /usr/bin/systemctl stop oniweb'
}

install_service() {
  log "installing oniweb.service (enabled, not started)"
  install -o root -g root -m 0644 "$DEPLOY_DIR/oniweb.service" /etc/systemd/system/oniweb.service
  install -d -o root -g root -m 0755 "$ENV_DIR"
  if [[ ! -e $ENV_FILE ]]; then
    install -o root -g root -m 0600 /dev/null "$ENV_FILE"
  fi
  systemctl daemon-reload
  systemctl enable oniweb
}

configure_caddy() {
  log "rendering Caddyfile for $ONI_DOMAIN"
  local tmp
  tmp="$(mktemp)"
  sed "s/__ONI_DOMAIN__/$ONI_DOMAIN/g" "$DEPLOY_DIR/Caddyfile" >"$tmp"
  if ! caddy validate --config "$tmp" --adapter caddyfile; then
    rm -f "$tmp"
    die "rendered Caddyfile is invalid"
  fi
  install -o root -g root -m 0644 "$tmp" /etc/caddy/Caddyfile
  rm -f "$tmp"
  systemctl enable caddy
  systemctl reload-or-restart caddy
}

configure_firewall() {
  log "configuring UFW (22, 80, 443)"
  ufw default deny incoming
  ufw default allow outgoing
  ufw allow 22/tcp
  ufw allow 80/tcp
  ufw allow 443/tcp
  ufw --force enable
}

create_admin_user() {
  log "creating admin user $ADMIN_USER"
  id -u "$ADMIN_USER" >/dev/null 2>&1 ||
    useradd --create-home --shell /bin/bash --groups sudo "$ADMIN_USER"

  local ssh_dir="/home/$ADMIN_USER/.ssh"
  install -d -o "$ADMIN_USER" -g "$ADMIN_USER" -m 0700 "$ssh_dir"
  # Seed from root's keys only if the admin has none yet; never overwrite existing keys.
  if [[ ! -s $ssh_dir/authorized_keys && -s /root/.ssh/authorized_keys ]]; then
    install -o "$ADMIN_USER" -g "$ADMIN_USER" -m 0600 /root/.ssh/authorized_keys "$ssh_dir/authorized_keys"
  fi
  # No password is set (key-only login), so sudo must not ask for one.
  install_sudoers "90-$ADMIN_USER" "$ADMIN_USER ALL=(ALL) NOPASSWD:ALL"
}

# Last step on purpose: root login is only turned off once the admin can get in.
harden_sshd() {
  [[ -s /home/$ADMIN_USER/.ssh/authorized_keys ]] ||
    die "$ADMIN_USER has no SSH keys; refusing to disable root login (add a key and re-run)"

  log "hardening sshd (no root login, no passwords)"
  # 00- sorts first: sshd keeps the first value it sees, and cloud-init's 50-*.conf may enable passwords.
  cat >"$SSHD_DROPIN" <<'EOF'
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
EOF
  if ! sshd -t; then
    rm -f "$SSHD_DROPIN"
    die "sshd rejected the hardening drop-in; removed it"
  fi
  systemctl reload ssh
}

main() {
  check_environment
  install_packages
  setup_swap
  create_service_users
  create_app_directories
  install_deploy_scripts
  configure_deploy_sudo
  install_service
  configure_caddy
  configure_firewall
  create_admin_user
  harden_sshd
  log "bootstrap complete; next: set SENTRY_DSN in $ENV_FILE, then deploy frontend before backend"
}

main "$@"
