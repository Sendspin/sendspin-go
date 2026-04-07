// ABOUTME: Clock synchronization using Kalman time filter
// ABOUTME: Provides NTP-style clock sync with offset and drift tracking
//
// Package sync provides clock synchronization for precise audio timing.
//
// Uses a two-dimensional Kalman filter to track both clock offset and drift
// between client and server, following the Sendspin time filter specification.
// NTP-style round-trip time measurements feed the filter for optimal estimation.
package sync
