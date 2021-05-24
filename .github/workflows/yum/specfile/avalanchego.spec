%define _build_id_links none

Name:           avalanchego
Version:        %{version}
Release:        %{release}
Summary:        The Avalanche platform binaries
URL:            https://github.com/ava-labs/%{name}
License:        BSD-3
AutoReqProv:    no

%description
Avalanche is an incredibly lightweight protocol, so the minimum computer requirements are quite modest.

%files
/usr/local/bin/avalanchego
/usr/local/lib/avalanchego
/usr/local/lib/avalanchego/evm

%changelog
* Mon Oct 26 2020 Charlie Wyse <charlie@avalabs.org>
- First creation of package

