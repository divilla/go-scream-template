#!/usr/bin/env perl
use strict;
use warnings;

use Cwd qw(abs_path);

sub fail {
    my ($message) = @_;
    die "$message\n";
}

sub run_checked {
    my (@command) = @_;
    system @command;
    $? == 0 or fail(join(" ", @command) . " failed");
}

sub capture_checked {
    my (@command) = @_;
    open my $output, "-|", @command or fail(join(" ", @command) . " failed");
    my $value = do { local $/; <$output> };
    close $output or fail(join(" ", @command) . " failed");

    return $value;
}

@ARGV <= 1 or fail('usage: scripts/commit-agent.pl ["commit message"]');

my $message = @ARGV == 1 ? $ARGV[0] : "Agent commit";
$message ne "" or fail("commit message cannot be empty");

my $script = abs_path($0);
defined $script or fail("resolve script path failed");
$script =~ s{/[^/]+\z}{};

my $repository = abs_path("$script/..");
defined $repository or fail("resolve repository root failed");
chdir $repository or fail("change directory to $repository failed");

my $branch = capture_checked(qw(git symbolic-ref --quiet --short HEAD));
chomp $branch;
$branch ne "" or fail("detached HEAD is not supported");

run_checked(qw(git add -A));
run_checked("git", "commit", "-m", $message);
run_checked("git", "push", "origin", $branch);
