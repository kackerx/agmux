#!/bin/sh
# usage: tap-formula.sh <tag> <sha256 of the tag's source tarball> [commit [date]]
# (commit and date are what --version says; the tarball has no .git to read them from)
# Prints Formula/agmux.rb for kackerx/homebrew-tap (release.yml): built from the
# tag's source with Go, macOS and Linux alike.
set -eu
tag=$1 sha=$2 commit=${3:-none} date=${4:-unknown}
cat <<RUBY
class Agmux < Formula
  desc "Card board for the coding agents in tmux, in a popup"
  homepage "https://github.com/kackerx/agmux"
  url "https://github.com/kackerx/agmux/archive/refs/tags/${tag}.tar.gz"
  sha256 "${sha}"
  license "MIT"

  depends_on "go" => :build

  def install
    ldflags = "-s -w -X main.version=v#{version} -X main.commit=${commit} -X main.date=${date}"
    system "go", "build", *std_go_args(ldflags:), "."
  end

  test do
    assert_match "agmux v#{version}", shell_output("#{bin}/agmux --version")
  end
end
RUBY
