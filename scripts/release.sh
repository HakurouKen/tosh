#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

fail() { printf '%s\n' "$*" >&2; exit 1; }

version=$(cat VERSION)
case "$version" in ''|*[!0-9.]*) fail 'VERSION 必须只包含 major.minor.patch。' ;; esac
printf '%s\n' "$version" | LC_ALL=C grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' ||
    fail 'VERSION 必须是 major.minor.patch 格式，且不能包含前导零。'
tag="v$version"

require_clean() {
    git rev-parse --verify HEAD >/dev/null
    [ -z "$(git status --porcelain --untracked-files=all)" ] || fail '工作区不干净，请先提交或清理修改。'
}

require_tag() {
    require_clean
    [ "$(git cat-file -t "refs/tags/$tag" 2>/dev/null || true)" = tag ] || fail "请先使用 make tag 创建 annotated tag $tag。"
    [ "$(git rev-parse "$tag^{commit}")" = "$(git rev-parse HEAD)" ] || fail "$tag 未指向当前提交。"
}

build() {
    go build -trimpath -ldflags "-X main.version=$version" -o "$1" ./cmd/tosh
}

dist() {
    # 临时目录仅用于本次打包；失败时不留下可被误认为完整的产物。
    stage=$(mktemp -d)
    trap 'rm -rf "$stage"' EXIT
    mkdir -p "$stage/output" "$stage/package/licenses"
    cp LICENSE "$stage/package/"
    for readme in README*.md; do
        [ ! -f "$readme" ] || cp "$readme" "$stage/package/"
    done
    [ ! -d docs ] || cp -R docs "$stage/package/"
    go list -deps -f '{{if .Module}}{{if not .Module.Main}}{{.Module.Path}} {{.Module.Dir}}{{end}}{{end}}' ./cmd/tosh > "$stage/modules.raw"
    sort -u "$stage/modules.raw" > "$stage/modules"
    while read -r module_path module_dir; do
        [ -n "$module_path" ] || continue
        mkdir -p "$stage/package/licenses/$module_path"
        for notice in "$module_dir"/LICENSE* "$module_dir"/COPYING* "$module_dir"/NOTICE*; do
            [ ! -f "$notice" ] || cp "$notice" "$stage/package/licenses/$module_path/"
        done
    done < "$stage/modules"
    for target in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do
        target_os=${target%/*}
        target_arch=${target#*/}
        printf '构建 %s %s\n' "$tag" "$target"
        CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" build "$stage/package/tosh"
        tar -czf "$stage/output/tosh_${version}_${target_os}_${target_arch}.tar.gz" -C "$stage/package" .
    done
    (
        cd "$stage/output"
        if command -v sha256sum >/dev/null 2>&1; then
            sha256sum ./*.tar.gz > checksums.txt
        else
            shasum -a 256 ./*.tar.gz > checksums.txt
        fi
    )
    mkdir -p dist
    # 每个版本独立存放，防止上传时混入其他版本的压缩包。
    rm -rf "dist/$tag"
    mv "$stage/output" "dist/$tag"
    printf '产物已生成：dist/%s\n' "$tag"
}

case "${1:-}" in
    version) printf '%s\n' "$version" ;;
    bump)
        part=${PART:-patch}
        case "$part" in patch|minor|major) ;; *) fail 'PART 必须是 patch、minor 或 major。' ;; esac
        next=$(printf '%s\n' "$version" | awk -F. -v part="$part" '{
            if (part == "major") printf "%.0f.0.0\n", $1+1;
            else if (part == "minor") printf "%s.%.0f.0\n", $1, $2+1;
            else printf "%s.%s.%.0f\n", $1, $2, $3+1;
        }')
        printf '%s\n' "$next" > VERSION
        printf '%s → %s（尚未提交）\n' "$version" "$next"
        ;;
    build) build bin/tosh ;;
    tag)
        require_clean
        if git show-ref --verify --quiet "refs/tags/$tag"; then
            fail "tag $tag 已存在，拒绝覆盖。"
        fi
        git tag -a "$tag" -m "Release $tag"
        ;;
    dist) dist ;;
    release)
        require_tag
        case "$(git remote get-url --push origin)" in
            https://github.com/HakurouKen/tosh|https://github.com/HakurouKen/tosh.git|git@github.com:HakurouKen/tosh|git@github.com:HakurouKen/tosh.git) ;;
            *) fail 'origin 必须指向 HakurouKen/tosh，避免推送 tag 与发布 Release 到不同仓库。' ;;
        esac
        gh auth status
        # 发布前检查当前提交，测试失败时不推送 tag、不创建 Release。
        go test -count=1 -race ./...
        go vet ./...
        dist
        require_tag
        git push origin "refs/tags/$tag:refs/tags/$tag"
        gh release create "$tag" "dist/$tag/"*.tar.gz "dist/$tag/checksums.txt" \
            --repo HakurouKen/tosh --verify-tag --title "$tag" --generate-notes
        ;;
    *) fail '用法：sh scripts/release.sh {version|bump|build|tag|dist|release}' ;;
esac
