#!/usr/bin/env bash
# Generate the package manifests for a release.
#
# A Scoop manifest and a PKGBUILD both pin a version and a checksum, so a copy
# kept by hand in the tree is wrong from the moment the next tag is pushed —
# and wrong in the way that matters, since the checksum is the thing users
# rely on. These are generated from the binaries the release job just built,
# and attached to the release itself. The version and the hashes are then
# correct by construction, and there is no second place to remember to update.
#
# Usage:  packaging/generate-packages.sh <version> <outdir>
#
# Expects the release binaries in the current directory under the exact names
# the release publishes: taskr, taskr-linux-arm64, taskr.exe.

set -euo pipefail

version="${1:?usage: generate-packages.sh <version> <outdir>}"
outdir="${2:?usage: generate-packages.sh <version> <outdir>}"
# Scoop and pacman both want a bare version; the release tag carries a "v".
bare="${version#v}"
base="https://github.com/Iliorn/taskr/releases/download/${version}"

mkdir -p "$outdir"

sha() { sha256sum "$1" | cut -d' ' -f1; }

sha_amd64="$(sha taskr)"
sha_arm64="$(sha taskr-linux-arm64)"
sha_windows="$(sha taskr.exe)"

# ── Scoop (Windows) ─────────────────────────────────────────────────────────
# Installed with:
#   scoop install https://github.com/Iliorn/taskr/releases/latest/download/taskr.json
# That URL always resolves to the newest release, so it never needs bumping.
# checkver/autoupdate are there so the manifest also works unmodified inside a
# Scoop bucket, where the excavator maintains it.
cat > "$outdir/taskr.json" <<EOF
{
    "version": "${bare}",
    "description": "A fast, keyboard-driven task manager for the terminal",
    "homepage": "https://github.com/Iliorn/taskr",
    "license": "MIT",
    "architecture": {
        "64bit": {
            "url": "${base}/taskr.exe",
            "hash": "${sha_windows}"
        }
    },
    "bin": "taskr.exe",
    "checkver": {
        "github": "https://github.com/Iliorn/taskr"
    },
    "autoupdate": {
        "architecture": {
            "64bit": {
                "url": "https://github.com/Iliorn/taskr/releases/download/v\$version/taskr.exe"
            }
        },
        "hash": {
            "url": "https://github.com/Iliorn/taskr/releases/download/v\$version/SHA256SUMS"
        }
    }
}
EOF

# ── AUR (Arch Linux) ────────────────────────────────────────────────────────
# taskr-bin installs the published binary rather than compiling, which is what
# most users of an -bin package want. The AUR keeps its own git repo, so this
# is the copy a maintainer pushes there — generated here so its pkgver and
# sha256sums can never disagree with the release they claim to describe.
cat > "$outdir/PKGBUILD" <<EOF
# Maintainer: Iliorn <https://github.com/Iliorn>
pkgname=taskr-bin
pkgver=${bare}
pkgrel=1
pkgdesc="A fast, keyboard-driven task manager for the terminal"
arch=('x86_64' 'aarch64')
url="https://github.com/Iliorn/taskr"
license=('MIT')
provides=('taskr')
conflicts=('taskr')
options=('!strip')

source_x86_64=("taskr-\${pkgver}-x86_64::${base}/taskr")
source_aarch64=("taskr-\${pkgver}-aarch64::${base}/taskr-linux-arm64")
source=("LICENSE-\${pkgver}::https://raw.githubusercontent.com/Iliorn/taskr/${version}/LICENSE")

sha256sums_x86_64=('${sha_amd64}')
sha256sums_aarch64=('${sha_arm64}')
sha256sums=('SKIP')

package() {
    install -Dm755 "\${srcdir}/taskr-\${pkgver}-\${CARCH}" "\${pkgdir}/usr/bin/taskr"
    install -Dm644 "\${srcdir}/LICENSE-\${pkgver}" "\${pkgdir}/usr/share/licenses/\${pkgname}/LICENSE"

    # The binary generates these itself, so they are always in step with the
    # commands it actually routes — no second copy to forget.
    "\${pkgdir}/usr/bin/taskr" completion bash | install -Dm644 /dev/stdin "\${pkgdir}/usr/share/bash-completion/completions/taskr"
    "\${pkgdir}/usr/bin/taskr" completion zsh  | install -Dm644 /dev/stdin "\${pkgdir}/usr/share/zsh/site-functions/_taskr"
    "\${pkgdir}/usr/bin/taskr" completion fish | install -Dm644 /dev/stdin "\${pkgdir}/usr/share/fish/vendor_completions.d/taskr.fish"
    "\${pkgdir}/usr/bin/taskr" man             | install -Dm644 /dev/stdin "\${pkgdir}/usr/share/man/man1/taskr.1"
}
EOF

echo "wrote $outdir/taskr.json and $outdir/PKGBUILD for $version"
