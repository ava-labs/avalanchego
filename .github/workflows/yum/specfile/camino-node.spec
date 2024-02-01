%define _build_id_links none

Name:           camino-node
Version:        %{version}
Release:        %{release}
Summary:        The Camino platform binaries
URL:            https://github.com/chain4travel/%{name}
License:        BSD-3
AutoReqProv:    no

%description
Camino is a lightweight protocol which runs on moderate computer requirements.

%files
/usr/local/bin/camino-node

%changelog
* Fri Apr 15 2022 devop <devop@chain4travel.com>
- Initial version
