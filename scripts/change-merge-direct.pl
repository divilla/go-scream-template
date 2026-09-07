#!/usr/bin/env perl
use strict;
use warnings;

sub fail {
    my ($message) = @_;
    die "$message\n";
}

@ARGV <= 1 or fail('usage: scripts/change-merge-direct.pl ["commit message"]');

my $branch = qx{git branch --show-current};
chomp $branch;
$branch ne "" or fail("detached HEAD is not supported");
$branch ne "master" or fail("cannot merge master into itself");

my $commit_message;
if (@ARGV == 1) {
    $commit_message = $ARGV[0];
    $commit_message ne "" or fail("commit message cannot be empty");
} else {
    my ($change_name) = $branch =~ m{^change/([0-9]+-[0-9A-Za-z_-]+)$}
        or fail("current branch is not a change/<change-slug> branch: $branch");
    $commit_message = "Implement change $change_name";
}

my $source_branch = $branch;

ensure_clean_worktree();
run_checked(qw(git fetch origin));
my $original_commit = trim(run_capture_checked(qw(git rev-parse HEAD)));
my $origin_source_commit = remote_head_commit($source_branch);
$original_commit eq $origin_source_commit
    or fail("local $source_branch $original_commit does not match origin/$source_branch $origin_source_commit");
my $base_branch = "master";
ensure_base_is_ancestor($base_branch);

my $squashed_commit = $original_commit;
if (!is_squashed_branch_commit($original_commit, $commit_message, $base_branch)) {
    $squashed_commit = create_squash_commit($commit_message, $base_branch);
    run_checked(
        "git",
        "push",
        "--force-with-lease=refs/heads/$source_branch:$origin_source_commit",
        "origin",
        "$squashed_commit:refs/heads/$source_branch",
    );
    run_checked("git", "update-ref", "refs/heads/$source_branch", $squashed_commit, $original_commit);
}

run_checked("git", "checkout", $base_branch);
run_checked("git", "pull", "--ff-only", "origin", $base_branch);
run_checked("git", "merge", "--ff-only", $source_branch);
my $base_commit = trim(run_capture_checked(qw(git rev-parse HEAD)));
$base_commit eq $squashed_commit
    or fail("$base_branch HEAD $base_commit does not match squashed branch commit $squashed_commit");
run_checked("git", "push", "-u", "origin", $base_branch);
my $origin_base_commit = remote_head_commit($base_branch);
$origin_base_commit eq $squashed_commit
    or fail("origin/$base_branch $origin_base_commit does not match squashed branch commit $squashed_commit");
run_checked(
    "git",
    "push",
    "--force-with-lease=refs/heads/$source_branch:$squashed_commit",
    "origin",
    "--delete",
    $source_branch,
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

sub is_squashed_branch_commit {
    my ($commit, $commit_message, $base_branch) = @_;
    my $parents = trim(run_capture_checked("git", "rev-list", "--parents", "-n", "1", $commit));
    my @fields = split /\s+/, $parents;
    shift @fields;
    return 0 if @fields != 1;

    my $base_commit = trim(run_capture_checked("git", "rev-parse", "origin/$base_branch"));
    return 0 if $fields[0] ne $base_commit;

    my $subject = trim(run_capture_checked("git", "log", "-1", "--format=%s", $commit));
    return $subject eq $commit_message;
}

sub create_squash_commit {
    my ($commit_message, $base_branch) = @_;
    my $tree = trim(run_capture_checked(qw(git rev-parse HEAD^{tree})));
    return trim(run_capture_checked(
        "git",
        "commit-tree",
        $tree,
        "-p",
        "origin/$base_branch",
        "-m",
        $commit_message,
    ));
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
