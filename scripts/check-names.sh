#!/usr/bin/env bash
# check-names.sh: stop private names (clients, people, internal hosts) from
# entering this public repo. The word list is private and never committed: it
# lives at $NAMES_GUARD_FILE or ~/.config/names-guard/terms.txt (one word or
# phrase per line, # comments allowed). Without it the check warns and passes.
# Words this repo legitimately uses go in .github/names-allow.txt.
#
# Usage:
#   scripts/check-names.sh --staged        added lines of the staged diff (pre-commit)
#   scripts/check-names.sh --msg FILE      a commit message file (commit-msg)
#   scripts/check-names.sh --range A..B    messages, authors and added lines of a commit range (pre-push)
#   scripts/check-names.sh --all           every tracked text file
# Matching is whole-word and case-insensitive. Exit 1 on a match.
set -euo pipefail

list="${NAMES_GUARD_FILE:-$HOME/.config/names-guard/terms.txt}"
if [ ! -r "$list" ]; then
  echo "check-names: no word list at $list, skipping" >&2
  exit 0
fi
root="$(git rev-parse --show-toplevel)"
allow="$root/.github/names-allow.txt"
mode="${1:---staged}"

# Input for the matcher: one "where<TAB>text" record per line.
added_lines() { # git diff args...
  git diff --no-color --no-ext-diff -U0 "$@" | awk '
    /^\+\+\+ / { f = substr($0, 7); next }
    /^\+/      { print f "\t" substr($0, 2) }'
}

feed() {
  case "$mode" in
    --staged) added_lines --cached ;;
    --msg)    awk '!/^#/ { print "commit message\t" $0 }' "${2:?--msg needs a file}" ;;
    --range)
      range="${2:?--range needs A..B}"
      git log --format='@@%h%n%an <%ae> %cn <%ce>%n%B' "$range" | awk '
        /^@@/ { c = substr($0, 3); next }
        { print "commit " c "\t" $0 }'
      added_lines "${range%%..*}" "${range##*..}" ;;
    --all)
      git ls-files -z | while IFS= read -r -d '' f; do
        [ -f "$f" ] && grep -Iq . "$f" 2>/dev/null || continue
        awk -v f="$f" '{ print f ":" NR "\t" $0 }' "$f"
      done ;;
    *) echo "usage: $0 [--staged|--msg FILE|--range A..B|--all]" >&2; exit 2 ;;
  esac
}

feed "$@" | NAMES_LIST="$list" NAMES_ALLOW="$allow" perl -e '
  sub words { my ($p) = @_; my @w; open(my $fh, "<", $p) or return ();
    while (<$fh>) { s/#.*//; s/^\s+|\s+$//g; push @w, lc $_ if length } return @w }
  my %allow = map { $_ => 1 } words($ENV{NAMES_ALLOW});
  my @terms = sort { length($b) <=> length($a) } grep { !$allow{$_} } words($ENV{NAMES_LIST});
  exit 0 unless @terms;
  my $re = join "|", map { quotemeta } @terms;
  $re = qr/(?<![A-Za-z0-9_])($re)(?![A-Za-z0-9_])/i;
  my $hits = 0;
  while (my $line = <STDIN>) {
    chomp $line; my ($where, $text) = split /\t/, $line, 2;
    next unless defined $text;
    while ($text =~ /$re/g) { print STDERR "check-names: $where: private word \"$1\"\n"; $hits++ }
  }
  if ($hits) { print STDERR "check-names: $hits match(es). Replace with a synthetic name (acme, example.com, alice) or, if the word is generic here, add it to .github/names-allow.txt.\n"; exit 1 }
'
