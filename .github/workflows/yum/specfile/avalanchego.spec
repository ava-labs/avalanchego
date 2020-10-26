%define _build_id_links none

Name:           avalanchego
Version:        0.1.0
Release:        1%{?dist}
Summary:        The Avalanche platform binaries
URL:            https://github.com/ava-labs/%{name}
License:        BSD-3

%description
Avalanche is an incredibly lightweight protocol, so the minimum computer requirements are quite modest.

%files
/usr/local/bin/avalanchego
/usr/local/lib/avalanchego
/usr/local/lib/avalanchego/evm

%changelog
* Mon Oct 26 2020 Charlie Wyse <charlie@avalabs.org>
- First creationg of package

