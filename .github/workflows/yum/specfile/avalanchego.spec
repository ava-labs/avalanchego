%define _build_id_links none

Name:           caminogo
Version:        %{version}
Release:        %{release}
Summary:        The Camino platform binaries
URL:            https://github.com/chain4travel/%{name}
License:        BSD-3
AutoReqProv:    no

%description
Camino is a lightweight protocol which runs on minimum computer requirements.

%files
/usr/local/bin/caminogo
/usr/local/lib/caminogo
/usr/local/lib/caminogo/evm

%changelog
* Fri Apr 15 2022 devop <devop@chain4travel.com>
- Initial version
