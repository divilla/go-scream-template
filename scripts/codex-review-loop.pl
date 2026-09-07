#!/usr/bin/env perl
use strict;
use warnings;

use Cwd qw(abs_path getcwd);
use Errno qw(EINTR);
use File::Basename qw(dirname);
use File::Spec;
use File::Temp qw(tempdir tempfile);
use IO::Handle;
use IO::Select;
use POSIX qw(WIFEXITED WIFSIGNALED WEXITSTATUS WTERMSIG setpgid);
use lib File::Spec->catdir(dirname(abs_path(__FILE__)), 'lib');
use APIHydra::Progress;

STDOUT->autoflush(1);
STDERR->autoflush(1);

my $active_codex_pid;
my $findings_dir;
my $findings_file;
my $fix_result_file;
my $active_output_log;
my $requested_exit_code = 0;

sub fail {
	my ($message) = @_;
	print STDERR "codex-review-loop: $message\n";
	exit 1;
}

sub read_file {
	my ($path) = @_;
	open my $handle, '<:raw', $path or fail("cannot read $path: $!");
	local $/;
	my $contents = <$handle>;
	close $handle or fail("cannot close $path: $!");
	return defined $contents ? $contents : '';
}

sub shell_quote {
	my ($argument) = @_;
	return $argument if $argument =~ m{\A[a-zA-Z0-9_\@%+=:,./-]+\z};
	$argument =~ s/'/'\\''/g;
	return "'$argument'";
}

sub shell_join {
	return join ' ', map { shell_quote($_) } @_;
}

sub capture_command {
	my ($quiet_stderr, @command) = @_;
	pipe my $reader, my $writer or fail("cannot create command pipe: $!");
	my $pid = fork();
	defined $pid or fail("cannot fork @command: $!");
	if ($pid == 0) {
		close $reader;
		open STDOUT, '>&', $writer or exit 127;
		if ($quiet_stderr) {
			open STDERR, '>', File::Spec->devnull() or exit 127;
		}
		close $writer;
		exec { $command[0] } @command;
		exit 127;
	}
	close $writer;
	local $/;
	my $output = <$reader>;
	close $reader;
	waitpid $pid, 0;
	my $status = command_exit_code($?);
	return (defined $output ? $output : '', $status);
}

sub command_exit_code {
	my ($status) = @_;
	return WEXITSTATUS($status) if WIFEXITED($status);
	return 128 + WTERMSIG($status) if WIFSIGNALED($status);
	return 1;
}

sub run_command {
	my (@command) = @_;
	system { $command[0] } @command;
	return command_exit_code($?);
}

sub command_exists {
	my ($command) = @_;
	for my $directory (File::Spec->path()) {
		my $candidate = File::Spec->catfile($directory, $command);
		return 1 if -f $candidate && -x $candidate;
	}
	return 0;
}

sub print_rendered_command {
	my ($command) = @_;
	my $separator_width = 1;
	for my $line (split /\n/, $command, -1) {
		$separator_width = length($line) if length($line) > $separator_width;
	}
	my $separator = '-' x $separator_width;
	print "$separator\n$command\n$separator\n";
	return $separator;
}

sub print_command {
	print_rendered_command(shell_join(@_));
}

sub print_command_with_input {
	my ($input_file, @command) = @_;
	print_rendered_command(shell_join(@command) . "\n< " . shell_quote($input_file));
}

sub print_file_block {
	my ($path) = @_;
	my $contents = read_file($path);
	print "\n$contents";
	print "\n" if $contents !~ /\n\z/;
	print "\n";
}

sub codex_session_id_from_output {
	my ($contents, $events_dir) = @_;
	my ($events, $events_path) = tempfile('codex-review-events.XXXXXX', DIR => $events_dir, UNLINK => 0);
	print {$events} $contents or fail("cannot write $events_path: $!");
	close $events or fail("cannot close $events_path: $!");
	my ($session_ids, $status) = capture_command(
		1,
		'jq',
		'--raw-input',
		'--raw-output',
		'fromjson? | select(type == "object" and .type == "thread.started") | .thread_id | strings',
		$events_path,
	);
	unlink $events_path;
	return undef if $status != 0;

	for my $session_id (split /\r?\n/, $session_ids) {
		next if $session_id !~ /\A[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\z/i;
		return $session_id;
	}
	return undef;
}

sub print_initial_context {
	my ($context, $terminal, $output) = @_;
	$terminal //= -t STDOUT;
	$output //= \*STDOUT;
	my ($label_color, $repository_color, $specification_color, $branch_color, $base_color, $options_color, $reset) = ('') x 7;
	if ($terminal && !exists $ENV{NO_COLOR}) {
		$label_color = "\033[37m";
		$repository_color = "\033[34m";
		$specification_color = "\033[35m";
		$branch_color = "\033[32m";
		$base_color = "\033[33m";
		$options_color = "\033[35m";
		$reset = "\033[0m";
	}

	print {$output} "\n";
	print {$output} $label_color, 'Repository:', $reset, ' ', $repository_color, $context->{repo_root}, $reset, "\n";
	print {$output} $label_color, 'Specification:', $reset, ' ', $specification_color, $context->{specification}, $reset, "\n";
	print {$output} $label_color, 'Branch:', $reset, ' ', $branch_color, $context->{branch}, $reset, "\n";
	print {$output} $label_color, 'Base:', $reset, ' ', $base_color, $context->{review_base}, $reset, "\n";
	print {$output} $label_color, 'Pinned base:', $reset, ' ', $base_color, $context->{review_base_commit}, $reset, "\n";
	print {$output} $label_color, 'Review options:', $reset, ' ', $options_color, $context->{review_options}, $reset, "\n";
	print {$output} $label_color, 'Findings:', $reset, ' ', $repository_color, $context->{findings_file}, $reset, "\n";
}

sub review_has_findings {
	my ($path) = @_;
	my $contents = read_file($path);
	return $contents =~ /^(?:Review comment:|Full review comments:)[\t ]*\r?$/m;
}

sub review_is_displayable {
	my ($path) = @_;
	my $contents = read_file($path);
	return 0 if $contents !~ /\S/;
	return 0 if $contents =~ /^(?:Reviewer failed to output a response\.|Review was interrupted\. Please re-run \/review and wait for it to complete\.)[\t ]*\r?$/m;
	return 1;
}

sub cleanup_findings {
	unlink $active_output_log if defined $active_output_log && -e $active_output_log;
	unlink $findings_file if defined $findings_file && -e $findings_file;
	unlink $fix_result_file if defined $fix_result_file && -e $fix_result_file;
	rmdir $findings_dir if defined $findings_dir && -d $findings_dir;
}

sub path_is_inside {
	my ($path, $parent) = @_;
	return 1 if $path eq $parent;
	return index($path, $parent . '/') == 0;
}

sub select_temp_root {
	my ($repo_root_physical) = @_;
	for my $candidate ($ENV{TMPDIR}, $ENV{TMP}, $ENV{TEMP}, '/tmp') {
		next if !defined $candidate || $candidate eq '' || !-d $candidate || !-w $candidate;
		my $resolved = abs_path($candidate);
		next if !defined $resolved || path_is_inside($resolved, $repo_root_physical);
		return $resolved;
	}
	return;
}

sub stop {
	my ($exit_code) = @_;
	$requested_exit_code = $exit_code;
	$SIG{INT} = 'DEFAULT';
	$SIG{TERM} = 'DEFAULT';
	if (defined $active_codex_pid) {
		kill 'TERM', -$active_codex_pid;
		return;
	}
	print STDERR "codex-review-loop: interrupted\n";
	exit $requested_exit_code;
}

sub start_command {
	my ($input_file, @command) = @_;
	pipe my $reader, my $writer or fail("cannot create Codex output pipe: $!");
	my $pid = fork();
	defined $pid or fail("cannot fork Codex command: $!");
	if ($pid == 0) {
		setpgid(0, 0) or exit 127;
		close $reader;
		if (defined $input_file) {
			open STDIN, '<:raw', $input_file or exit 127;
		}
		open STDOUT, '>&', $writer or exit 127;
		open STDERR, '>&', $writer or exit 127;
		close $writer;
		exec { $command[0] } @command;
		exit 127;
	}
	close $writer;
	setpgid($pid, $pid);
	return ($pid, $reader);
}

sub monitor_command {
	my ($progress, $input_file, $output_log, $output_handler, @command) = @_;
	my ($pid, $reader) = start_command($input_file, @command);
	$active_codex_pid = $pid;
	my $selector = IO::Select->new($reader);
	my $next_render_at = $progress->started_at();
	my $pending_output = '';
	$progress->render($next_render_at);

	open my $log, '>:raw', $output_log or fail("cannot create $output_log: $!");
	while ($selector->count) {
		my $now = $progress->now();
		if ($now >= $next_render_at) {
			$progress->render($now);
			$next_render_at += $progress->render_interval() while $next_render_at <= $now;
		}
		my $timeout = $next_render_at - $progress->now();
		$timeout = 0 if $timeout < 0;
		my @ready = $selector->can_read($timeout);
		next if !@ready;

		my $buffer = '';
		my $bytes = sysread $reader, $buffer, 64 * 1024;
		if (!defined $bytes) {
			next if $! == EINTR;
			close $log;
			fail("cannot read Codex output: $!");
		}
		if ($bytes == 0) {
			$selector->remove($reader);
			close $reader;
			next;
		}
		print {$log} $buffer or fail("cannot write $output_log: $!");
		if (defined $output_handler) {
			$pending_output .= $buffer;
			while ($pending_output =~ s/\A(.*?\n)//s) {
				$output_handler->($1);
			}
		}
		$progress->record_output();
	}
	$output_handler->($pending_output) if defined $output_handler && $pending_output ne '';
	close $log or fail("cannot close $output_log: $!");

	waitpid $pid, 0;
	my $exit_code = command_exit_code($?);
	$active_codex_pid = undef;
	return $exit_code;
}

sub run_codex {
	my ($input_file, @command) = @_;
	my $command_separator;
	if (defined $input_file) {
		$command_separator = print_command_with_input($input_file, @command);
	} else {
		$command_separator = print_command(@command);
	}

	my $terminal = -t STDOUT;
	my $progress = APIHydra::Progress->new(terminal => $terminal, output => \*STDOUT);
	my $is_review = @command >= 3 && $command[0] eq 'codex' && $command[1] eq 'exec' && $command[2] eq 'review';
	my $session_id;
	my $output_handler;
	if ($is_review) {
		$output_handler = sub {
			return if defined $session_id;
			$session_id = codex_session_id_from_output($_[0], $findings_dir);
			return if !defined $session_id;
			print "\r\033[2K" if $terminal;
			print "codex-session-id: $session_id\n$command_separator\n";
		};
	}
	$active_output_log = File::Spec->catfile($findings_dir, 'codex-output.log');
	my $exit_code = monitor_command($progress, $input_file, $active_output_log, $output_handler, @command);
	if ($requested_exit_code != 0) {
		$progress->finish(0);
		unlink $active_output_log;
		$active_output_log = undef;
		print STDERR "codex-review-loop: interrupted\n";
		exit $requested_exit_code;
	}

	if ($is_review && !defined $session_id && $exit_code == 0) {
		$progress->finish(0);
		fail('Codex review output did not contain a thread.started session id');
	}

	$progress->finish($exit_code == 0);
	if ($exit_code != 0) {
		print STDERR "codex-review-loop: Codex command failed with exit code $exit_code\n";
		if (-s $active_output_log) {
			my $output = read_file($active_output_log);
			$output =~ s/^/  /mg;
			print STDERR "codex-review-loop: Codex command output:\n$output";
		}
	}
	unlink $active_output_log;
	$active_output_log = undef;
	return $exit_code;
}

sub commit_review_fix {
	my ($branch, $message) = @_;
	my $status = run_command('git', 'add', '-A');
	exit $status if $status != 0;
	$status = run_command('git', 'commit', '-m', $message);
	exit $status if $status != 0;
	$status = run_command('git', 'push', 'origin', $branch);
	exit $status if $status != 0;
}

sub parse_review_options {
	my (@arguments) = @_;
	my $review_base = '';
	my @review_arguments;
	my %options_with_values = map { $_ => 1 } qw(
		-c --config
		--disable --enable
		-m --model
		--title
	);
	while (@arguments) {
		my $argument = shift @arguments;
		if ($argument eq '--') {
			fail('custom review instructions cannot be combined with --base');
		} elsif ($argument eq '-o' || $argument =~ /\A-o.+/ || $argument eq '--output-last-message' || $argument =~ /\A--output-last-message=/) {
			fail('-o/--output-last-message is managed by this script and always writes findings.md');
		} elsif ($argument eq '--output-schema' || $argument =~ /\A--output-schema=/) {
			fail('--output-schema is ignored by codex exec review');
		} elsif ($argument eq '--uncommitted' || $argument =~ /\A--uncommitted=/) {
			fail('--uncommitted cannot include committed fixes; use --base BRANCH');
		} elsif ($argument eq '--commit' || $argument =~ /\A--commit=/) {
			fail('--commit cannot include later fix commits; use --base BRANCH');
		} elsif ($argument eq '--base') {
			@arguments or fail('--base requires a branch');
			$review_base eq '' or fail('multiple --base options are not supported');
			$review_base = shift @arguments;
			$review_base ne '' && $review_base !~ /\A--/ or fail('--base requires a branch');
		} elsif ($argument =~ /\A--base=(.*)\z/s) {
			$review_base eq '' or fail('multiple --base options are not supported');
			$review_base = $1;
			$review_base ne '' or fail('--base requires a branch');
		} elsif ($argument eq '-' || $argument !~ /\A-/) {
			fail('custom review instructions cannot be combined with --base');
		} elsif ($options_with_values{$argument}) {
			@arguments or fail("$argument requires a value");
			push @review_arguments, $argument, shift @arguments;
		} else {
			push @review_arguments, $argument;
		}
	}
	return ($review_base, @review_arguments);
}

sub main {
	my (@arguments) = @_;
	command_exists('codex') or fail('codex is not installed or is not on PATH');
	command_exists('git') or fail('git is not installed or is not on PATH');
	command_exists('jq') or fail('jq is not installed or is not on PATH');

	my $script_dir = abs_path(dirname($0));
	my ($repo_root, $repo_status) = capture_command(1, 'git', '-C', $script_dir, 'rev-parse', '--show-toplevel');
	$repo_status == 0 or fail('scripts directory is not inside a git repository');
	$repo_root =~ s/\s+\z//;
	chdir $repo_root or fail("cannot enter repository $repo_root: $!");
	my $repo_root_physical = abs_path(getcwd());

	@arguments && $arguments[0] !~ /\A-/ or fail('usage: codex-review-loop.pl SPECIFICATION [review options]');
	my $specification = shift @arguments;
	-f $specification or fail("specification file not found: $specification");
	my $fix_prompt = '$change-fix-findings ' . $specification
		. ' Do not commit or push; the caller handles commits.';

	my $temp_root = select_temp_root($repo_root_physical);
	defined $temp_root or fail('cannot find a writable temporary directory outside the repository');
	$findings_dir = tempdir('apih-review.XXXXXXXXXX', DIR => $temp_root, CLEANUP => 0);
	defined $findings_dir or fail("cannot create private findings directory under $temp_root");
	$findings_file = File::Spec->catfile($findings_dir, 'findings.md');
	$fix_result_file = File::Spec->catfile($findings_dir, 'fix-result.md');

	my ($branch, $branch_status) = capture_command(0, 'git', 'branch', '--show-current');
	$branch_status == 0 or exit $branch_status;
	$branch =~ s/\s+\z//;
	$branch ne '' or fail('cannot commit review fixes from detached HEAD');

	my ($worktree_changes, $status) = capture_command(0, 'git', 'status', '--porcelain=v1', '--untracked-files=all', '--', '.');
	$status == 0 or exit $status;
	$worktree_changes eq '' or fail('working tree must be clean');

	my ($review_base, @review_arguments) = parse_review_options(@arguments);
	if ($review_base eq '') {
		($review_base, $status) = capture_command(1, 'git', 'symbolic-ref', '--quiet', '--short', 'refs/remotes/origin/HEAD');
		$review_base =~ s/\s+\z//;
		$status == 0 && $review_base ne '' or fail("cannot resolve origin's default branch; supply --base explicitly");
	}

	my ($review_base_commit, $resolve_status) = capture_command(1, 'git', 'rev-parse', '--verify', '--end-of-options', "$review_base^{commit}");
	$review_base_commit =~ s/\s+\z//;
	$resolve_status == 0 or fail("cannot resolve review base $review_base to a commit");
	@review_arguments = ('--base', $review_base_commit, @review_arguments);

	print_initial_context({
		branch             => $branch,
		findings_file      => $findings_file,
		repo_root          => $repo_root,
		review_base        => $review_base,
		review_base_commit => $review_base_commit,
		review_options     => shell_join(@review_arguments),
		specification      => $specification,
	});

	my $pass = 1;
	my $fix_number = 1;
	while (1) {
		printf "\n=== Review pass %02d ===\n", $pass;
		unlink $findings_file;
		my @review_command = ('codex', 'exec', 'review', '--json', @review_arguments, '-o', $findings_file);
		$status = run_codex(undef, @review_command);
		exit $status if $status != 0;
		-f $findings_file or fail('review did not write findings.md');

		print_file_block($findings_file);
		review_is_displayable($findings_file) or fail('review did not produce a displayable result');

		if (!review_has_findings($findings_file)) {
			print "Changed files:\n  (none)\n";
			print "Review complete: no review comments found.\n";
			unlink $findings_file;
			return 0;
		}

		my ($before_fix, $before_status) = capture_command(0, 'git', 'rev-parse', 'HEAD');
		$before_status == 0 or exit $before_status;
		$before_fix =~ s/\s+\z//;
		unlink $fix_result_file;
		printf "=== Fix findings %02d ===\n", $fix_number;
		$status = run_codex($findings_file, 'codex', 'exec', '--json', '-o', $fix_result_file, $fix_prompt);
		exit $status if $status != 0;
		my ($after_fix, $after_status) = capture_command(0, 'git', 'rev-parse', 'HEAD');
		$after_status == 0 or exit $after_status;
		$after_fix =~ s/\s+\z//;
		$after_fix eq $before_fix or fail(sprintf 'codex created a commit; expected this script to create review fixes %02d', $fix_number);
		-f $fix_result_file or fail('fix did not write a final response');
		print_file_block($fix_result_file);

		my ($changed_files, $changed_status) = capture_command(0, 'git', 'status', '--short', '--untracked-files=all', '--', '.');
		$changed_status == 0 or exit $changed_status;
		print "Changed files:\n";
		if ($changed_files ne '') {
			print $changed_files;
		} else {
			print "  (none)\n";
			fail('codex made no repository changes; see the fix result above');
		}

		my $commit_message = sprintf 'review fixes %02d', $fix_number;
		print "Commit: $commit_message\n";
		unlink $findings_file, $fix_result_file;
		commit_review_fix($branch, $commit_message);
		$pass++;
		$fix_number++;
	}
}

$SIG{INT} = sub { stop(130) };
$SIG{TERM} = sub { stop(143) };

END {
	cleanup_findings();
}

exit main(@ARGV) unless caller;

1;
