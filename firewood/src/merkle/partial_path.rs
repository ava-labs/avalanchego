use std::fmt::{self, Debug};

/// PartialPath keeps a list of nibbles to represent a path on the Trie.
#[derive(PartialEq, Eq, Clone)]
pub struct PartialPath(pub Vec<u8>);

impl Debug for PartialPath {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        for nib in self.0.iter() {
            write!(f, "{:x}", *nib & 0xf)?;
        }
        Ok(())
    }
}

impl std::ops::Deref for PartialPath {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        &self.0
    }
}

impl PartialPath {
    pub fn into_inner(self) -> Vec<u8> {
        self.0
    }

    pub(super) fn encode(&self, term: bool) -> Vec<u8> {
        let odd_len = (self.0.len() & 1) as u8;
        let flags = if term { 2 } else { 0 } + odd_len;
        let mut res = if odd_len == 1 {
            vec![flags]
        } else {
            vec![flags, 0x0]
        };
        res.extend(&self.0);
        res
    }

    pub fn decode<R: AsRef<[u8]>>(raw: R) -> (Self, bool) {
        let raw = raw.as_ref();
        let term = raw[0] > 1;
        let odd_len = raw[0] & 1;
        (
            Self(if odd_len == 1 {
                raw[1..].to_vec()
            } else {
                raw[2..].to_vec()
            }),
            term,
        )
    }

    pub(super) fn dehydrated_len(&self) -> u64 {
        let len = self.0.len() as u64;
        if len & 1 == 1 {
            (len + 1) >> 1
        } else {
            (len >> 1) + 1
        }
    }
}
