package APIHydra::Progress;

use strict;
use warnings;

use Time::HiRes qw(CLOCK_MONOTONIC clock_gettime);

my @ACTIVITY_FRAMES = ("\xC2\xB7", "\xE2\x80\xA2", "\xE2\x97\x8F", "\xE2\x80\xA2");
my $OUTPUT_MARKER = "\xE2\x80\xA2";
my $SUCCESS_SYMBOL = "\xE2\x9C\x85";
my $FAILURE_SYMBOL = "\xE2\x9D\x8C";
my $ACTIVITY_INTERVAL = 0.25;
my $OUTPUT_MARKER_INTERVAL = 1;

sub new {
	my ($class, %arguments) = @_;
	return bless {
		active         => 1,
		markers        => 0,
		last_marker_at => undef,
		output         => $arguments{output},
		started_at     => $arguments{started_at} // clock_gettime(CLOCK_MONOTONIC),
		terminal       => $arguments{terminal},
	}, $class;
}

sub now {
	return clock_gettime(CLOCK_MONOTONIC);
}

sub started_at {
	my ($self) = @_;
	return $self->{started_at};
}

sub render_interval {
	return $ACTIVITY_INTERVAL;
}

sub activity_frame {
	my ($self, $elapsed) = @_;
	my $frame_index = int($elapsed / $ACTIVITY_INTERVAL) % scalar @ACTIVITY_FRAMES;
	return $ACTIVITY_FRAMES[$frame_index];
}

sub record_output {
	my ($self, $now) = @_;
	$now //= $self->now();
	if (!defined $self->{last_marker_at} || $now - $self->{last_marker_at} >= $OUTPUT_MARKER_INTERVAL) {
		$self->{markers}++;
		$self->{last_marker_at} = $now;
	}
}

sub _text {
	my ($self, $status, $now) = @_;
	$now //= $self->now();
	my $elapsed = int($now - $self->{started_at});
	my $markers = $OUTPUT_MARKER x $self->{markers};
	return sprintf '[%s] %02d:%02d%s',
		$status, int($elapsed / 60), $elapsed % 60, $markers eq '' ? '' : " $markers";
}

sub render {
	my ($self, $now) = @_;
	return if !$self->{terminal};
	$now //= $self->now();
	my $frame = $self->activity_frame($now - $self->{started_at});
	my $separator = $self->{markers} == 0 ? ' ' : '';
	print { $self->{output} } "\r\033[2K", $self->_text(' ', $now), $separator, $frame;
}

sub finish {
	my ($self, $success, $now) = @_;
	return if !$self->{active};
	my $status = $success ? $SUCCESS_SYMBOL : $FAILURE_SYMBOL;
	my $prefix = $self->{terminal} ? "\r\033[2K" : '';
	print { $self->{output} } $prefix, $self->_text($status, $now), "\n";
	$self->{active} = 0;
}

1;
