#!/usr/bin/env perl
use strict;
use warnings;

use Cwd qw(abs_path);
use File::Basename qw(dirname);
use File::Spec;
use File::Temp qw(tempfile);

my $tests = 0;

sub assert_equal {
	my ($got, $want, $message) = @_;
	$tests++;
	die "$message: got [$got], want [$want]\n" if $got ne $want;
}

my $script_dir = dirname(__FILE__);
my $script = abs_path(File::Spec->catfile($script_dir, 'codex-code-spec.pl'));
do $script or die "cannot load $script: " . ($@ || $!);

my $context_output = '';
open my $context_handle, '>:raw', \$context_output or die "cannot create context capture: $!";
{
	local $ENV{NO_COLOR};
	delete $ENV{NO_COLOR};
	print_initial_context({
		branch        => 'change/004-decoder-service',
		repo_root     => '/home/vito/go/src/apihydra',
		specification => 'agent/specs/004-decoder-service.md',
	}, 1, $context_handle);
}
close $context_handle;
assert_equal(
	$context_output,
	"\n\033[37mRepository:\033[0m \033[34m/home/vito/go/src/apihydra\033[0m\n"
		. "\033[37mSpecification:\033[0m \033[35magent/specs/004-decoder-service.md\033[0m\n"
		. "\033[37mBranch:\033[0m \033[32mchange/004-decoder-service\033[0m\n",
	'implementation context uses the requested order, spacing, and colors',
);

my $command_output = '';
open my $command_handle, '>:raw', \$command_output or die "cannot create command capture: $!";
{
	local *STDOUT = $command_handle;
	print_rendered_command('codex exec');
}
close $command_handle;
assert_equal(
	$command_output,
	('-' x length('codex exec')) . "\ncodex exec\n" . ('-' x length('codex exec')) . "\n",
	'implementation command separators match the rendered command width',
);

my $formatted = '';
open my $formatted_output, '>:raw', \$formatted or die "cannot create formatted progress capture: $!";
my $formatted_progress = APIHydra::Progress->new(
	terminal   => 1,
	output     => $formatted_output,
	started_at => 100,
);
assert_equal($formatted_progress->activity_frame(0), "\xC2\xB7", 'animation starts at the small central point');
assert_equal($formatted_progress->activity_frame(0.25), "\xE2\x80\xA2", 'animation advances after 250 ms');
assert_equal($formatted_progress->activity_frame(0.50), "\xE2\x97\x8F", 'animation reaches the large central point');
assert_equal($formatted_progress->activity_frame(0.75), "\xE2\x80\xA2", 'animation contracts after 750 ms');
assert_equal($formatted_progress->activity_frame(1), "\xC2\xB7", 'animation repeats after one second');
for my $second (100 .. 108) {
	$formatted_progress->record_output($second);
}
$formatted_progress->render(108.5);
for my $second (109 .. 111) {
	$formatted_progress->record_output($second);
}
$formatted_progress->finish(1, 771);
close $formatted_output;
my $bullet = "\xE2\x80\xA2";
my $circle = "\xE2\x97\x8F";
my $check = "\xE2\x9C\x85";
assert_equal(
	$formatted,
	"\r\033[2K[ ] 00:08 " . ($bullet x 9) . $circle
		. "\r\033[2K[$check] 11:11 " . ($bullet x 12) . "\n",
	'active and finished progress use the exact compact format',
);

my $rendered = '';
open my $progress_output, '>:raw', \$rendered or die "cannot create progress capture: $!";
my ($log, $output_log) = tempfile();
close $log;
my $progress = APIHydra::Progress->new(terminal => 1, output => $progress_output);
my $status = monitor_command(
	$progress,
	$output_log,
	$^X,
	'-e',
	'select undef, undef, undef, 0.85',
);
close $progress_output;
unlink $output_log;

assert_equal($status, 0, 'silent child exits successfully');
my %rendered_frames;
for my $render (split /\r/, $rendered) {
	for my $frame ("\xC2\xB7", "\xE2\x80\xA2", "\xE2\x97\x8F") {
		$rendered_frames{$frame} = 1 if $render =~ /\Q$frame\E\z/;
	}
}
assert_equal(scalar keys %rendered_frames, 3, 'silent child renders distinct animation frames at timed intervals');

print "codex-code-spec unit tests passed ($tests assertions)\n";
