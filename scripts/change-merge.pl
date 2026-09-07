#!/usr/bin/env perl
use strict;
use warnings;

sub fail {
    my ($message) = @_;
    die "$message\n";
}

@ARGV == 0 or fail("usage: scripts/change-merge.pl");

my $branch = qx{git branch --show-current};
chomp $branch;

my ($change_name) = $branch =~ m{^change/([0-9]+-[0-9A-Za-z_-]+)$}
    or fail("current branch is not a change/<change-slug> branch: $branch");

my $change_branch = "change/$change_name";

ensure_clean_worktree();
run_checked(qw(git fetch origin));
my $original_commit = trim(run_capture_checked(qw(git rev-parse HEAD)));
my $origin_change_commit = remote_head_commit($change_branch);
$original_commit eq $origin_change_commit
    or fail("local $change_branch $original_commit does not match origin/$change_branch $origin_change_commit");
my $base_branch = pull_request_base($change_branch);
ensure_base_is_ancestor($base_branch);

my $squashed_commit = $original_commit;
if (!is_squashed_change_commit($original_commit, $change_name, $base_branch)) {
    $squashed_commit = create_squash_commit($change_name, $base_branch);
    run_checked(
        "git",
        "push",
        "--force-with-lease=refs/heads/$change_branch:$origin_change_commit",
        "origin",
        "$squashed_commit:refs/heads/$change_branch",
    );
    run_checked("git", "update-ref", "refs/heads/$change_branch", $squashed_commit, $original_commit);
}

my $verified_base_branch = pull_request_base($change_branch);
$verified_base_branch eq $base_branch
    or fail("PR base branch changed from $base_branch to $verified_base_branch");

run_checked("git", "checkout", $base_branch);
run_checked("git", "pull", "--ff-only", "origin", $base_branch);
run_checked("git", "merge", "--ff-only", $change_branch);
my $base_commit = trim(run_capture_checked(qw(git rev-parse HEAD)));
$base_commit eq $squashed_commit
    or fail("$base_branch HEAD $base_commit does not match squashed change commit $squashed_commit");
run_checked("git", "push", "-u", "origin", $base_branch);
my $origin_base_commit = remote_head_commit($base_branch);
$origin_base_commit eq $squashed_commit
    or fail("origin/$base_branch $origin_base_commit does not match squashed change commit $squashed_commit");
run_checked(
    "git",
    "push",
    "--force-with-lease=refs/heads/$change_branch:$squashed_commit",
    "origin",
    "--delete",
    $change_branch,
);

sub ensure_clean_worktree {
    my $status = run_capture_checked(qw(git status --short));
    trim($status) eq "" or fail("uncommitted changes");
}

sub run_capture_checked {
    my (@command) = @_;
    open my $fh, "-|", @command
        or fail(join(" ", @command) . " failed to start: $!");
    local $/;
    my $output = <$fh>;
    $output = "" if !defined $output;
    close $fh or fail(join(" ", @command) . " failed");
    return $output;
}

sub trim {
    my ($value) = @_;
    $value =~ s/\A\s+//;
    $value =~ s/\s+\z//;
    return $value;
}

sub remote_head_commit {
    my ($branch_name) = @_;
    my $output = trim(run_capture_checked("git", "ls-remote", "--heads", "origin", $branch_name));
    $output =~ /\A([0-9a-f]{40})\s+refs\/heads\/\Q$branch_name\E\z/
        or fail("cannot verify origin/$branch_name");
    return $1;
}

sub is_squashed_change_commit {
    my ($commit, $change_name, $base_branch) = @_;
    my $parents = trim(run_capture_checked("git", "rev-list", "--parents", "-n", "1", $commit));
    my @fields = split /\s+/, $parents;
    shift @fields;
    return 0 if @fields != 1;

    my $base_commit = trim(run_capture_checked("git", "rev-parse", "origin/$base_branch"));
    return 0 if $fields[0] ne $base_commit;

    my $subject = trim(run_capture_checked("git", "log", "-1", "--format=%s", $commit));
    return $subject eq "Implement change $change_name";
}

sub create_squash_commit {
    my ($change_name, $base_branch) = @_;
    my $tree = trim(run_capture_checked(qw(git rev-parse HEAD^{tree})));
    return trim(run_capture_checked(
        "git",
        "commit-tree",
        $tree,
        "-p",
        "origin/$base_branch",
        "-m",
        "Implement change $change_name",
    ));
}

sub pull_request_base {
    my ($change_branch) = @_;
    my $details = trim(run_capture_checked(
        "gh",
        "pr",
        "view",
        $change_branch,
        "--json",
        "state,baseRefName",
        "--jq",
        '.state + "\t" + .baseRefName',
    ));
    my ($state, $base_branch) = split /\t/, $details, 2;
    $state eq "OPEN"
        or fail("PR for $change_branch is not open");
    defined $base_branch && $base_branch ne ""
        or fail("PR for $change_branch has no base branch");
    my $validated = trim(run_capture_checked("git", "check-ref-format", "--branch", $base_branch));
    $validated eq $base_branch
        or fail("PR for $change_branch has invalid base branch: $base_branch");
    return $base_branch;
}

sub run_checked {
    my (@command) = @_;
    system @command;
    $? == 0 or fail(join(" ", @command) . " failed");
}

sub ensure_base_is_ancestor {
    my ($base_branch) = @_;
    system("git", "merge-base", "--is-ancestor", "origin/$base_branch", "HEAD");
    return if $? == 0;
    fail("rebase needed: origin/$base_branch is not an ancestor of HEAD");
}
