#include "sqlite3mc.c"

static int aicatGenerateRawKey(void* cipher, char* key, int length, int rekey, unsigned char* salt)
{
  AES128Cipher* aes = (AES128Cipher*)cipher;
  if (length != KEYLENGTH_AES128) return SQLITE_MISUSE;
  memcpy(aes->m_key, key, KEYLENGTH_AES128);
  return SQLITE_OK;
}

SQLITE_API int aicat_register_wecom_cipher(void)
{
  CipherDescriptor cipher = mcAES128Descriptor;
  cipher.m_name = "aicat_wecom_aes128";
  cipher.m_generateKey = aicatGenerateRawKey;
  return sqlite3mc_register_cipher(&cipher, mcAES128Params, 0);
}
