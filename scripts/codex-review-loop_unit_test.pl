#!/usr/bin/env perl
use strict;
use warnings;

use Cwd qw(abs_path);
use File::Basename qw(dirname);
use File::Spec;
use File::Temp qw(tempdir tempfile);

my $tests = 0;

sub assert_equal {
	my ($got, $want, $message) = @_;
	$tests++;
	die "$message: got [$got], want [$want]\n" if $got ne $want;
}

sub assert_undefined {
	my ($got, $message) = @_;
	$tests++;
	die "$message: got [$got], want undef\n" if defined $got;
}

my $script = abs_path(File::Spec->catfile(dirname(__FILE__), 'codex-review-loop.pl'));
do $script or die "cannot load $script: " . ($@ || $!);

{
	local $ENV{PATH} = File::Spec->catdir(File::Spec->rootdir(), 'path-that-does-not-exist');
	assert_equal(command_exists('jq'), 0, 'dependency lookup rejects an absent jq executable');
}

my $context_output = '';
open my $context_handle, '>:raw', \$context_output or die "cannot create context capture: $!";
{
	local $ENV{NO_COLOR};
	delete $ENV{NO_COLOR};
	print_initial_context({
		branch             => 'change/004-decoder-service',
		findings_file      => '/tmp/apih-review.test/findings.md',
		repo_root          => '/home/vito/go/src/apihydra',
		review_base        => 'origin/master',
		review_base_commit => '47d4f35c2759e81216efb909c66d7e58ba43e503',
		review_options     => '--base 47d4f35c2759e81216efb909c66d7e58ba43e503',
		specification      => 'agent/specs/004-decoder-service.md',
	}, 1, $context_handle);
}
close $context_handle;
assert_equal(
	$context_output,
	"\n\033[37mRepository:\033[0m \033[34m/home/vito/go/src/apihydra\033[0m\n"
		. "\033[37mSpecification:\033[0m \033[35magent/specs/004-decoder-service.md\033[0m\n"
		. "\033[37mBranch:\033[0m \033[32mchange/004-decoder-service\033[0m\n"
		. "\033[37mBase:\033[0m \033[33morigin/master\033[0m\n"
		. "\033[37mPinned base:\033[0m \033[33m47d4f35c2759e81216efb909c66d7e58ba43e503\033[0m\n"
		. "\033[37mReview options:\033[0m \033[35m--base 47d4f35c2759e81216efb909c66d7e58ba43e503\033[0m\n"
		. "\033[37mFindings:\033[0m \033[34m/tmp/apih-review.test/findings.md\033[0m\n",
	'review context extends the implementation order with consistent colors',
);

my $command_output = '';
open my $command_handle, '>:raw', \$command_output or die "cannot create command capture: $!";
{
	local *STDOUT = $command_handle;
	print_rendered_command("codex exec --json\n< findings.md");
}
close $command_handle;
my $command_separator = '-' x length('codex exec --json');
assert_equal(
	$command_output,
	"$command_separator\ncodex exec --json\n< findings.md\n$command_separator\n",
	'multiline review commands use the longest line without extending the separator',
);

my $session_id = '123e4567-e89b-12d3-a456-426614174000';
my $events_dir = tempdir(CLEANUP => 1);
assert_equal(
	codex_session_id_from_output(
		"not json\n"
			. "{\"type\":\"thread.started\",not valid json}\n"
			. "{\"type\":\"turn.started\",\"thread_id\":\"$session_id\"}\n"
			. "{\"thread_id\":\"$session_id\",\"sequence\":1,\"type\":\"thread.started\"}\n",
		$events_dir,
	),
	$session_id,
	'the thread.started event supplies the Codex session id independently of property order',
);
assert_undefined(
	codex_session_id_from_output("{\"type\":\"thread.started\",\"thread_id\":\"not-a-uuid\"}\n", $events_dir),
	'an invalid thread id is not rendered as a Codex session id',
);

my $checkout_tmp = tempdir(CLEANUP => 1);
my $observed_events_path;
{
	no warnings qw(once redefine);
	local $ENV{TMPDIR} = $checkout_tmp;
	local *capture_command = sub {
		$observed_events_path = $_[-1];
		return ("$session_id\n", 0);
	};
	assert_equal(
		codex_session_id_from_output("{\"type\":\"thread.started\",\"thread_id\":\"$session_id\"}\n", $events_dir),
		$session_id,
		'the session parser returns an id when TMPDIR points inside the checkout',
	);
}
assert_equal(dirname($observed_events_path), $events_dir, 'event input is created in the safe findings directory');
opendir my $checkout_tmp_handle, $checkout_tmp or die "cannot inspect $checkout_tmp: $!";
my @checkout_tmp_entries = grep { $_ ne '.' && $_ ne '..' } readdir $checkout_tmp_handle;
closedir $checkout_tmp_handle or die "cannot close $checkout_tmp: $!";
assert_equal(join("\n", @checkout_tmp_entries), '', 'event parsing creates no artifacts in checkout TMPDIR');

my $stream_output = '';
open my $stream_progress_output, '>:raw', \$stream_output or die "cannot create stream progress capture: $!";
my ($stream_log, $stream_output_log) = tempfile();
close $stream_log;
my @handled_output;
my $stream_status = monitor_command(
	APIHydra::Progress->new(terminal => 0, output => $stream_progress_output),
	undef,
	$stream_output_log,
	sub { push @handled_output, @_ },
	$^X,
	'-e',
	'$| = 1; print "partial"; select undef, undef, undef, 0.05; print " line\nremainder"',
);
close $stream_progress_output;
unlink $stream_output_log;
assert_equal($stream_status, 0, 'streaming-output child exits successfully');
assert_equal(
	join('', @handled_output),
	"partial line\nremainder",
	'the output handler receives complete lines and the final partial line while output streams',
);

assert_equal(
	join("\0", parse_review_options('--model', 'gpt-5', '--title', 'Review title', '--base', 'develop')),
	join("\0", 'develop', '--model', 'gpt-5', '--title', 'Review title'),
	'option values remain distinct from positional review prompts',
);

my $rendered = '';
open my $progress_output, '>:raw', \$rendered or die "cannot create progress capture: $!";
my ($log, $output_log) = tempfile();
close $log;
my $progress = APIHydra::Progress->new(terminal => 1, output => $progress_output);
my $status = monitor_command(
	$progress,
	undef,
	$output_log,
	undef,
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

print "codex-review-loop unit tests passed ($tests assertions)\n";
