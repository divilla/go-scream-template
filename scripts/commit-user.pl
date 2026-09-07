#!/usr/bin/env perl
use strict;
use warnings;

use Cwd qw(abs_path);

sub fail {
    my ($message) = @_;
    die "$message\n";
}

@ARGV <= 1 or fail('usage: scripts/commit-user.pl ["commit message"]');

my $message = @ARGV == 1 ? $ARGV[0] : "User commit";
$message ne "" or fail("commit message cannot be empty");

my $script = abs_path($0);
defined $script or fail("resolve script path failed");
$script =~ s{/[^/]+\z}{};

exec($^X, "$script/commit-agent.pl", $message)
    or fail("execute commit-agent.pl failed: $!");
