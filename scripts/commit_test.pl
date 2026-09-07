#!/usr/bin/env perl
use strict;
use warnings;

use Cwd qw(abs_path getcwd);
use File::Basename qw(dirname);
use File::Copy qw(copy);
use File::Path qw(make_path);
use File::Temp qw(tempdir);
use IPC::Open3;
use Symbol qw(gensym);

my $tests = 0;

sub assert_equal {
    my ($got, $want, $message) = @_;
    $tests++;
    die "$message: got [$got], want [$want]\n" if $got ne $want;
}

my $source = dirname(abs_path(__FILE__));
my $root = tempdir(CLEANUP => 1);
my $repository = "$root/repo with spaces";
make_path("$repository/scripts", "$root/bin", "$root/outside");
for my $name (qw(commit-user.pl commit-agent.pl)) {
    copy("$source/$name", "$repository/scripts/$name") or die "copy $name: $!";
}
open my $git, '>', "$root/bin/git" or die "create git: $!";
print {$git} <<'GIT';
#!/usr/bin/env perl
use strict;
use warnings;
use Cwd qw(getcwd);
open my $log, '>>', $ENV{COMMIT_TEST_LOG} or die $!;
print {$log} join("\0", getcwd(), @ARGV, '', '');
close $log or die $!;
exit 23 if $ARGV[0] eq $ENV{COMMIT_TEST_FAIL};
if ($ARGV[0] eq 'symbolic-ref') {
    print $ENV{COMMIT_TEST_EMPTY_BRANCH} ? "" : "change/021-refactor\n";
}
GIT
close $git or die "close git: $!";
chmod 0755, "$root/bin/git" or die "chmod git: $!";
local $ENV{PATH} = "$root/bin:$ENV{PATH}";
local $ENV{COMMIT_TEST_LOG} = "$root/git.log";

for my $author (qw(user agent)) {
    my $script = "commit-$author.pl";
    my $default = ucfirst($author) . ' commit';
    my $custom = "Quotes ' \" and spaces\nsecond line \$literal `literal`";
    for my $case (
        {name => 'default', args => [], message => $default},
        {name => 'custom', args => [$custom], message => $custom},
        {name => 'empty', args => [''], calls => 0, code => 255, error => "commit message cannot be empty\n"},
        {name => 'excess', args => ['one', 'two'], calls => 0, code => 255, error => "usage: scripts/$script [\"commit message\"]\n"},
        {name => 'detached', args => [], fail => 'symbolic-ref', calls => 1, code => 23, error => "git symbolic-ref --quiet --short HEAD failed\n"},
        {name => 'empty branch', args => [], empty_branch => 1, calls => 1, code => 255, error => "detached HEAD is not supported\n"},
        {name => 'add failure', args => [], fail => 'add', calls => 2, code => 23, error => "git add -A failed\n"},
        {name => 'commit failure', args => [], fail => 'commit', calls => 3, code => 23, error => "git commit -m $default failed\n"},
        {name => 'push failure', args => [], fail => 'push', calls => 4, code => 23, error => "git push origin change/021-refactor failed\n"},
    ) {
        unlink $ENV{COMMIT_TEST_LOG};
        local $ENV{COMMIT_TEST_FAIL} = $case->{fail} // '';
        local $ENV{COMMIT_TEST_EMPTY_BRANCH} = $case->{empty_branch} // 0;
        my $previous = getcwd();
        chdir "$root/outside" or die "chdir: $!";
        my $stderr = gensym;
        my $pid = open3(undef, my $stdout, $stderr, $^X, "$repository/scripts/$script", @{$case->{args}});
        my $output = do { local $/; <$stdout> };
        my $error = do { local $/; <$stderr> };
        waitpid $pid, 0;
        my $code = $? >> 8;
        chdir $previous or die "restore cwd: $!";
        assert_equal($code, $case->{code} // 0, 'exit status');
        assert_equal($output, '', 'stdout');
        assert_equal($error, $case->{error} // '', 'diagnostic');

        my $calls = '';
        if (-e $ENV{COMMIT_TEST_LOG}) {
            open my $log, '<', $ENV{COMMIT_TEST_LOG} or die "read log: $!";
            $calls = do { local $/; <$log> };
            close $log;
        }
        my @expected = (
            ['symbolic-ref', '--quiet', '--short', 'HEAD'],
            ['add', '-A'],
            ['commit', '-m', $case->{message} // $default],
            ['push', 'origin', 'change/021-refactor'],
        );
        splice @expected, $case->{calls} if defined $case->{calls};
        assert_equal($calls, join('', map { join("\0", $repository, @$_, '', '') } @expected),
            "$script $case->{name}: arguments, cwd, order, fail-fast");
    }
}

print "commit tests passed ($tests assertions)\n";
