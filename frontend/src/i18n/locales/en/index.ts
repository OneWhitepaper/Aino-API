import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import workspace from './workspace'
import contact from './contact'
import iqRadar from './iqRadar'
import iqTest from './iqTest'

export default {
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...batchImage,
  admin,
  ...misc,
  ...workspace,
  ...contact,
  ...iqRadar,
  ...iqTest,
}
